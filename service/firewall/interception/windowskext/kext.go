//go:build windows
// +build windows

package windowskext

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
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

	// initialize and start driver service
	var err error
	service, err = createKextService(driverName, driverPath)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	running, err := service.isRunning()
	if err == nil && !running {
		err = service.start(true)

		if err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("service not initialized: %w", err)
	}

	// Open the driver
	filename := `\\.\` + driverName
	kextHandle, err = openDriver(filename)

	// driver was not installed
	if err != nil {
		return fmt.Errorf("failed to open driver: %q %w", filename, err)
	}

	return nil
}

func SetKextHandler(handle windows.Handle) {
	kextHandle = handle
}

func SetKextService(handle windows.Handle, path string) {
	service = &KextService{handle: handle}
	driverPath = path
}

// Stop intercepting.
func Stop() error {
	// Prepare kernel for shutdown
	err := shutdownRequest()
	if err != nil {
		log.Warningf("winkext: shutdown request failed: %s", err)
	}

	kextLock.Lock()
	defer kextLock.Unlock()

	err = closeDriver(kextHandle)
	if err != nil {
		log.Warningf("winkext: failed to close the handle: %s", err)
	}

	err = service.stop(true)
	if err != nil {
		log.Warningf("winkext: failed to stop service: %s", err)
	}
	// Driver file may change on the next start so it's better to delete the service
	err = service.delete()
	if err != nil {
		log.Warningf("winkext: failed to delete service: %s", err)
	}

	kextHandle = winInvalidHandleValue
	return nil
}

func shutdownRequest() error {
	kextLock.RLock()
	defer kextLock.RUnlock()
	if kextHandle == winInvalidHandleValue {
		return ErrKextNotReady
	}
	// Sent a shutdown request so the kernel extension can prepare.
	_, err := deviceIOControl(kextHandle, IOCTL_SHUTDOWN_REQUEST, nil, nil)

	return err
}

// RecvVerdictRequest waits for the next verdict request from the kext. If a timeout is reached, both *VerdictRequest and error will be nil.
func RecvVerdictRequest() (*VerdictRequest, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()
	if kextHandle == winInvalidHandleValue {
		return nil, ErrKextNotReady
	}

	// DEBUG:
	// timestamp := time.Now()
	// defer log.Tracef("winkext: getting verdict request took %s", time.Since(timestamp))

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

	return &new, nil
}

// SetVerdict sets the verdict for a packet and/or connection.
func SetVerdict(pkt *Packet, verdict network.Verdict) error {
	if pkt.verdictRequest.pid != 0 {
		return nil // Ignore info only packets
	}
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

func UpdateVerdict(conn *network.Connection) error {
	kextLock.RLock()
	defer kextLock.RUnlock()

	// Check if driver is initialized
	if kextHandle == winInvalidHandleValue {
		log.Error("kext: failed to clear the cache: kext not ready")
		return ErrKextNotReady
	}

	var isIpv6 uint8 = 0
	if conn.IPVersion == packet.IPv6 {
		isIpv6 = 1
	}

	// initialize variables
	info := VerdictUpdateInfo{
		ipV6:       isIpv6,
		protocol:   uint8(conn.IPProtocol),
		localIP:    ipAddressToArray(conn.LocalIP, isIpv6 == 1),
		localPort:  conn.LocalPort,
		remoteIP:   ipAddressToArray(conn.Entity.IP, isIpv6 == 1),
		remotePort: conn.Entity.Port,
		verdict:    uint8(conn.Verdict),
	}

	// Make driver request
	data := asByteArray(&info)
	_, err := deviceIOControl(kextHandle, IOCTL_UPDATE_VERDICT, data, nil)
	return err
}

func GetVersion() (*VersionInfo, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()

	// Check if driver is initialized
	if kextHandle == winInvalidHandleValue {
		log.Error("kext: failed to clear the cache: kext not ready")
		return nil, ErrKextNotReady
	}

	data := make([]uint8, 4)
	_, err := deviceIOControl(kextHandle, IOCTL_VERSION, nil, data)

	if err != nil {
		return nil, err
	}

	version := &VersionInfo{
		major:    data[0],
		minor:    data[1],
		revision: data[2],
		build:    data[3],
	}
	return version, nil
}

var sizeOfConnectionStat = uint32(unsafe.Sizeof(ConnectionStat{}))

func GetConnectionsStats() ([]ConnectionStat, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()

	// Check if driver is initialized
	if kextHandle == winInvalidHandleValue {
		log.Error("kext: failed to clear the cache: kext not ready")
		return nil, ErrKextNotReady
	}

	var data [100]ConnectionStat
	size := len(data)
	bytesReturned, err := deviceIOControl(kextHandle, IOCTL_GET_CONNECTIONS_STAT, asByteArray(&size), asByteArray(&data))

	if err != nil {
		return nil, err
	}

	return data[:bytesReturned/sizeOfConnectionStat], nil
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
