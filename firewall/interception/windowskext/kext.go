//go:build windows
// +build windows

package windowskext

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"golang.org/x/sys/windows"
)

// Package errors
var (
	ErrKextNotReady = errors.New("the windows kernel extension (driver) is not ready to accept commands")
	ErrNoPacketID   = errors.New("the packet has no ID, possibly because it was fast-tracked by the kernel extension")

	kextLock   sync.RWMutex
	driverPath string

	kextHandle windows.Handle
	service    *KextService
)

const (
	winErrInvalidData     = uintptr(windows.ERROR_INVALID_DATA)
	winInvalidHandleValue = windows.Handle(^uintptr(0)) // Max value
	driverName            = "PortmasterKext"
)

// Init initializes the DLL and the Kext (Kernel Driver).
func Init(path string) error {
	kextHandle = winInvalidHandleValue
	driverPath = path
	return nil
}

// Start intercepting.
func Start() error {
	kextLock.Lock()
	defer kextLock.Unlock()

	filename := `\\.\` + driverName

	// check if driver is already installed
	var err error
	kextHandle, err = openDriver(filename)
	if err == nil {
		return nil // device was already initialized
	}

	// initialize and start driver service
	service, err = createKextService(driverName, driverPath)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	err = service.start()

	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	// open the driver
	kextHandle, err = openDriver(filename)

	// driver was not installed
	if err != nil {
		return fmt.Errorf("failed to open driver: %q %w", filename, err)
	}

	return nil
}

// Stop intercepting.
func Stop() error {
	kextLock.Lock()
	defer kextLock.Unlock()

	err := closeDriver(kextHandle)
	if err != nil {
		log.Warningf("winkext: failed to close the handle: %s", err)
	}

	err = service.stop()
	if err != nil {
		log.Warningf("winkext: failed to stop service: %s", err)
	}
	// Driver file may change on the next start so it's better to delete the service
	err = service.delete()
	if err != nil {
		log.Warningf("winkext: failed to delete service: %s", err)
	}
	err = service.closeHandle()
	if err != nil {
		log.Warningf("winkext: failed to close the handle: %s", err)
	}

	kextHandle = winInvalidHandleValue
	return nil
}

// RecvVerdictRequest waits for the next verdict request from the kext. If a timeout is reached, both *VerdictRequest and error will be nil.
func RecvVerdictRequest() (*VerdictRequest, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()
	if kextHandle == winInvalidHandleValue {
		return nil, ErrKextNotReady
	}

	timestamp := time.Now()
	// Initialize struct for the output data
	var new VerdictRequest

	// Make driver request
	data := asByteArray(&new)
	bytesRead, err := deviceIOControl(kextHandle, IOCTL_RECV_VERDICT_REQ, nil, data)
	if err != nil {
		return nil, err
	}
	if bytesRead == 0 {
		return nil, nil // no error, no new verdict request
	}

	log.Tracef("winkext: getting verdict request took %s", time.Now().Sub(timestamp))
	return &new, nil
}

// SetVerdict sets the verdict for a packet and/or connection.
func SetVerdict(pkt *Packet, verdict network.Verdict) error {
	if pkt.verdictRequest.id == 0 {
		log.Tracer(pkt.Ctx()).Errorf("kext: failed to set verdict %s: no packet ID", verdict)
		return ErrNoPacketID
	}

	kextLock.RLock()
	defer kextLock.RUnlock()
	if kextHandle == winInvalidHandleValue {
		log.Tracer(pkt.Ctx()).Errorf("kext: failed to set verdict %s: kext not ready", verdict)
		return ErrKextNotReady
	}

	verdictInfo := VerdictInfo{pkt.verdictRequest.id, verdict}

	// Make driver request
	data := asByteArray(&verdictInfo)
	_, err := deviceIOControl(kextHandle, IOCTL_SET_VERDICT, data, nil)
	if err != nil {
		log.Tracer(pkt.Ctx()).Errorf("kext: failed to set verdict %s on packet %d", verdict, pkt.verdictRequest.id)
		return err
	}
	return nil
}

// GetPayload returns the payload of a packet.
func GetPayload(packetID uint32, packetSize uint32) ([]byte, error) {
	if packetID == 0 {
		return nil, ErrNoPacketID
	}

	// Check if driver is initialized
	kextLock.RLock()
	defer kextLock.RUnlock()
	if kextHandle == winInvalidHandleValue {
		return nil, ErrKextNotReady
	}

	buf := make([]byte, packetSize)

	// Combine id and length
	payload := struct {
		id     uint32
		length uint32
	}{packetID, packetSize}

	// Make driver request
	data := asByteArray(&payload)
	bytesRead, err := deviceIOControl(kextHandle, IOCTL_GET_PAYLOAD, data, unsafe.Slice(&buf[0], packetSize))

	if err != nil {
		return nil, err
	}

	// check the result and return
	if bytesRead == 0 {
		return nil, errors.New("windows kext did not return any data")
	}

	if bytesRead < uint32(len(buf)) {
		return buf[:bytesRead], nil
	}

	return buf, nil
}

func ClearCache() error {
	kextLock.RLock()
	defer kextLock.RUnlock()

	// Check if driver is initialized
	if kextHandle == winInvalidHandleValue {
		log.Error("kext: failed to clear the cache: kext not ready")
		return ErrKextNotReady
	}

	// Make driver request
	_, err := deviceIOControl(kextHandle, IOCTL_CLEAR_CACHE, nil, nil)
	return err
}

func asByteArray[T any](obj *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(obj)), unsafe.Sizeof(*obj))
}

func openDriver(filename string) (windows.Handle, error) {
	u16filename, err := syscall.UTF16FromString(filename)
	if err != nil {
		return winInvalidHandleValue, fmt.Errorf("failed to convert driver filename to UTF16 string %w", err)
	}

	handle, err := windows.CreateFile(&u16filename[0], windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return winInvalidHandleValue, err
	}

	return handle, nil
}

func closeDriver(handle windows.Handle) error {
	if kextHandle == winInvalidHandleValue {
		return ErrKextNotReady
	}

	return windows.CloseHandle(handle)
}
