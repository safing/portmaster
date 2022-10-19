//go:build windows
// +build windows

package windowskext

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/tevino/abool"
	"golang.org/x/sys/windows"
)

// Package errors
var (
	ErrKextNotReady = errors.New("the windows kernel extension (driver) is not ready to accept commands")
	ErrNoPacketID   = errors.New("the packet has no ID, possibly because it was fast-tracked by the kernel extension")

	winErrInvalidData = uintptr(windows.ERROR_INVALID_DATA)

	kextLock       sync.RWMutex
	ready          = abool.NewBool(false)
	urgentRequests *int32
	driverPath     string

	kextHandle windows.Handle
)

const driverName = "PortmasterKext"

func init() {
	var urgentRequestsValue int32
	urgentRequests = &urgentRequestsValue
}

// Init initializes the DLL and the Kext (Kernel Driver).
func Init(dllPath, path string) error {
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
	service, err := driverInstall(driverPath)
	if err != nil {
		return fmt.Errorf("Failed to start service: %s", err)
	}

	// open the driver
	kextHandle, err = openDriver(filename)

	// close the service handles
	windows.DeleteService(service)
	windows.CloseServiceHandle(service)

	// driver was not installed
	if err != nil {
		return fmt.Errorf("Failed to start the kext service: %s %q", err, filename)
	}

	ready.Set()
	return nil
}

// Stop intercepting.
func Stop() error {
	kextLock.Lock()
	defer kextLock.Unlock()
	if !ready.IsSet() {
		return ErrKextNotReady
	}
	ready.UnSet()

	err := closeDriver(kextHandle)
	if err != nil {
		log.Errorf("winkext: failed to close the handle: %s", err)
	}
	_, _ = exec.Command("sc", "stop", driverName).Output()
	return nil
}

// RecvVerdictRequest waits for the next verdict request from the kext. If a timeout is reached, both *VerdictRequest and error will be nil.
func RecvVerdictRequest() (*VerdictRequest, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()
	if !ready.IsSet() {
		return nil, ErrKextNotReady
	}
	// wait for urgent requests to complete
	for i := 1; i <= 100; i++ {
		if atomic.LoadInt32(urgentRequests) <= 0 {
			break
		}
		if i == 100 {
			log.Warningf("winkext: RecvVerdictRequest waited 100 times")
		}
		time.Sleep(100 * time.Microsecond)
	}

	timestamp := time.Now()
	var new VerdictRequest

	data := asByteArray(&new)
	bytesRead, err := deviceIoControlRead(kextHandle, IOCTL_RECV_VERDICT_REQ, data)
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
	if !ready.IsSet() {
		log.Tracer(pkt.Ctx()).Errorf("kext: failed to set verdict %s: kext not ready", verdict)
		return ErrKextNotReady
	}

	verdictInfo := struct {
		id      uint32
		verdict network.Verdict
	}{pkt.verdictRequest.id, verdict}

	atomic.AddInt32(urgentRequests, 1)
	data := asByteArray(&verdictInfo)
	_, err := deviceIoControlWrite(kextHandle, IOCTL_SET_VERDICT, data)
	atomic.AddInt32(urgentRequests, -1)
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

	kextLock.RLock()
	defer kextLock.RUnlock()
	if !ready.IsSet() {
		return nil, ErrKextNotReady
	}

	buf := make([]byte, packetSize)

	payload := struct {
		id     uint32
		length uint32
	}{packetID, packetSize}

	atomic.AddInt32(urgentRequests, 1)
	data := asByteArray(&payload)
	bytesRead, err := deviceIoControlReadWrite(kextHandle, IOCTL_GET_PAYLOAD, data, unsafe.Slice(&buf[0], packetSize))

	atomic.AddInt32(urgentRequests, -1)

	if err != nil {
		return nil, err
	}

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
	if !ready.IsSet() {
		log.Error("kext: failed to clear the cache: kext not ready")
		return ErrKextNotReady
	}

	_, err := deviceIoControlRead(kextHandle, IOCTL_CLEAR_CACHE, nil)
	return err
}

func asByteArray[T any](obj *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(obj)), unsafe.Sizeof(*obj))
}
