//go:build windows
// +build windows

package windowskext

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
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

	filename := `\\.\PortmasterKext`

	u16fname, err := syscall.UTF16FromString(filename)
	if err != nil {
		return fmt.Errorf("Bad filename: %s", err)
	}

	u16DriverPath, err := syscall.UTF16FromString(driverPath)
	if err != nil {
		return fmt.Errorf("Bad driver path: %s", err)
	}
	kextHandle, err = windows.CreateFile(&u16fname[0], windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		return nil // All good
	}

	service, err := portmasterDriverInstall(&u16DriverPath[0])
	if err != nil {
		return fmt.Errorf("Faield to start service: %s", err)
	}

	kextHandle, err = windows.CreateFile(&u16fname[0],
		windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, 0, 0)

	windows.DeleteService(service)
	windows.CloseServiceHandle(service)

	if err != nil {
		return fmt.Errorf("Faield to kext service: %s %q", err, filename)
	}

	ready.Set()
	testRead()
	return nil
}

func testRead() {
	buf := [5]byte{1, 2, 3, 4, 5}
	_, err := deviceIoControl(IOCTL_TEST, &buf[0], uintptr(len(buf)))
	if err != nil {
		log.Criticalf("Erro reading test data: %s", err)
	}

	log.Criticalf("Read restul: %v", buf)
}

func createService(manager windows.Handle, portmasterKextPath *uint16) (windows.Handle, error) {
	u16fname, err := syscall.UTF16FromString("PortmasterKext")
	if err != nil {
		return 0, fmt.Errorf("Bad service: %s", err)
	}
	service, err := windows.OpenService(manager, &u16fname[0], windows.SERVICE_ALL_ACCESS)
	if err == nil {
		return service, nil
	}
	service, err = windows.CreateService(manager, &u16fname[0], &u16fname[0], windows.SERVICE_ALL_ACCESS, windows.SERVICE_KERNEL_DRIVER, windows.SERVICE_DEMAND_START, windows.SERVICE_ERROR_NORMAL, portmasterKextPath, nil, nil, nil, nil, nil)
	if err != nil {
		return 0, err
	}

	return service, nil
}

func portmasterDriverInstall(portmasterKextPath *uint16) (windows.Handle, error) {
	// Open the service manager:
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS)
	if err != nil {
		return 0, fmt.Errorf("Failed to open service manager: %d", err)
	}
	defer windows.CloseServiceHandle(manager)

	var service windows.Handle
retryLoop:
	for i := 0; i < 3; i++ {
		service, err = createService(manager, portmasterKextPath)
		if err == nil {
			break retryLoop
		}
	}

	if err != nil {
		return 0, fmt.Errorf("Failed to create service: %s", err)
	}

	err = windows.StartService(service, 0, nil)
	// Start the service:
	if err != nil {
		err = windows.GetLastError()
		if err == windows.ERROR_SERVICE_ALREADY_RUNNING {
			// windows.SetLastError(0)
			// windows.SetLast
		} else {
			// Failed to start service; clean-up:
			var status windows.SERVICE_STATUS
			_ = windows.ControlService(service, windows.SERVICE_CONTROL_STOP, &status)
			_ = windows.DeleteService(service)
			_ = windows.CloseServiceHandle(service)
			service = 0
			//windows.SetLastError(err)
		}
	}

	return service, nil
}

// Stop intercepting.
func Stop() error {
	kextLock.Lock()
	defer kextLock.Unlock()
	if !ready.IsSet() {
		return ErrKextNotReady
	}
	ready.UnSet()

	err := windows.CloseHandle(kextHandle)
	if err != nil {
		log.Errorf("kext: faield to close handle: %s", err)
	}
	_, _ = exec.Command("sc", "stop", "PortmasterKext").Output()
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

	data := (*byte)(unsafe.Pointer(&new))
	_, err := deviceIoControl(IOCTL_RECV_VERDICT_REQ, data, unsafe.Sizeof(new))
	if err != nil {
		return nil, err
	}
	log.Tracef("winkext: getting verdict request took %s", time.Now().Sub(timestamp))

	log.Criticalf("%v", new)
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
	_, err := deviceIoControlBufferd(IOCTL_SET_VERDICT,
		(*byte)(unsafe.Pointer(&verdictInfo)), unsafe.Sizeof(verdictInfo), nil, 0)
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

	writenSize, err := deviceIoControlBufferd(IOCTL_GET_PAYLOAD,
		(*byte)(unsafe.Pointer(&payload)), unsafe.Sizeof(payload),
		&buf[0], uintptr(packetSize))

	// timestamp := time.Now()
	// log.Tracef("winkext: getting payload for packetID %d took %s", packetID, time.Now().Sub(timestamp))
	atomic.AddInt32(urgentRequests, -1)

	if err != nil {
		return nil, err
	}

	if writenSize == 0 {
		return nil, errors.New("windows kext did not return any data")
	}

	if writenSize < uint32(len(buf)) {
		return buf[:writenSize], nil
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

	_, err := deviceIoControl(IOCTL_CLEAR_CACHE, nil, 0)
	return err
}
