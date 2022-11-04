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
func Init(path string) error {
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
	_ = windows.DeleteService(service)
	_ = windows.CloseServiceHandle(service)

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

	_, err = exec.Command("sc", "stop", driverName).Output() // This is a question of taste, but it is a robust and solid solution
	if err != nil {
		log.Errorf("winkext: failed to stop the service: %q", err)
	}
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
	// Initialize struct for the output data
	var new VerdictRequest

	// Make driver request
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

	verdictInfo := VerdictInfo{pkt.verdictRequest.id, verdict}

	// Make driver request
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

	// Check if driver is initialized
	kextLock.RLock()
	defer kextLock.RUnlock()
	if !ready.IsSet() {
		return nil, ErrKextNotReady
	}

	buf := make([]byte, packetSize)

	// Combine id and length
	payload := struct {
		id     uint32
		length uint32
	}{packetID, packetSize}

	// Make driver request
	atomic.AddInt32(urgentRequests, 1)
	data := asByteArray(&payload)
	bytesRead, err := deviceIoControlReadWrite(kextHandle, IOCTL_GET_PAYLOAD, data, unsafe.Slice(&buf[0], packetSize))

	atomic.AddInt32(urgentRequests, -1)

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
	if !ready.IsSet() {
		log.Error("kext: failed to clear the cache: kext not ready")
		return ErrKextNotReady
	}

	// Make driver request
	_, err := deviceIoControlRead(kextHandle, IOCTL_CLEAR_CACHE, nil)
	return err
}

func UpdateVerdict(conn *network.Connection) error {
	kextLock.RLock()
	defer kextLock.RUnlock()

	// Check if driver is initialized
	if !ready.IsSet() {
		log.Error("kext: failed to clear the cache: kext not ready")
		return ErrKextNotReady
	}

	// initialize variables
	info := &VerdictUpdateInfo{
		ipV6:       uint8(conn.IPVersion),
		protocol:   uint8(conn.IPProtocol),
		localPort:  conn.LocalPort,
		remotePort: conn.Entity.Port,
		verdict:    uint8(conn.Verdict.Active),
	}

	// copy ip addresses
	copy(asByteArray(&info.localIP[0]), conn.LocalIP)
	copy(asByteArray(&info.remoteIP[0]), conn.Entity.IP)

	// Make driver request
	data := asByteArray(&info)
	err := deviceIoControlDirect(kextHandle, IOCTL_UPDATE_VERDICT, data)
	return err
}

func GetVersion() (*VersionInfo, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()

	// Check if driver is initialized
	if !ready.IsSet() {
		log.Error("kext: failed to clear the cache: kext not ready")
		return nil, ErrKextNotReady
	}

	data := make([]uint8, 4)
	err := deviceIoControlDirect(kextHandle, IOCTL_VERSION, data)

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

func asByteArray[T any](obj *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(obj)), unsafe.Sizeof(*obj))
}
