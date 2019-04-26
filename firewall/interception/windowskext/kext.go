package windowskext

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Safing/portmaster/network"
	"github.com/tevino/abool"
	"golang.org/x/sys/windows"
)

// Package errors
var (
	ErrKextNotReady = errors.New("the windows kernel extension (driver) is not ready to accept commands")

	kext     *WinKext
	kextLock sync.RWMutex
	ready    = abool.NewBool(false)
)

// WinKext holds the DLL handle.
type WinKext struct {
	sync.RWMutex

	dll        *windows.DLL
	driverPath string

	init               *windows.Proc
	start              *windows.Proc
	stop               *windows.Proc
	recvVerdictRequest *windows.Proc
	setVerdict         *windows.Proc
	getPayload         *windows.Proc
}

// Init initializes the DLL and the Kext (Kernel Driver).
func Init(dllPath, driverPath string) error {

	new := &WinKext{
		driverPath: driverPath,
	}

	var err error

	// load dll
	new.dll, err = windows.LoadDLL(dllPath)
	if err != nil {
		return err
	}

	// load functions
	new.init, err = new.dll.FindProc("PortmasterInit")
	if err != nil {
		return fmt.Errorf("could not find proc PortmasterStart in dll: %s", err)
	}
	new.start, err = new.dll.FindProc("PortmasterStart")
	if err != nil {
		return fmt.Errorf("could not find proc PortmasterStart in dll: %s", err)
	}
	new.stop, err = new.dll.FindProc("PortmasterStop")
	if err != nil {
		return fmt.Errorf("could not find proc PortmasterStop in dll: %s", err)
	}
	new.recvVerdictRequest, err = new.dll.FindProc("PortmasterRecvVerdictRequest")
	if err != nil {
		return fmt.Errorf("could not find proc PortmasterRecvVerdictRequest in dll: %s", err)
	}
	new.setVerdict, err = new.dll.FindProc("PortmasterSetVerdict")
	if err != nil {
		return fmt.Errorf("could not find proc PortmasterSetVerdict in dll: %s", err)
	}
	new.getPayload, err = new.dll.FindProc("PortmasterGetPayload")
	if err != nil {
		return fmt.Errorf("could not find proc PortmasterGetPayload in dll: %s", err)
	}

	// initialize dll/kext
	rc, _, lastErr := new.init.Call()
	if rc != windows.NO_ERROR {
		return formatErr(lastErr)
	}

	// set kext
	kextLock.Lock()
	defer kextLock.Unlock()
	kext = new

	return nil
}

// Start intercepting.
func Start() error {
	kextLock.Lock()
	defer kextLock.Unlock()

	// convert to C string
	charArray := make([]byte, len(kext.driverPath)+1)
	copy(charArray, []byte(kext.driverPath))
	charArray[len(charArray)-1] = 0 // force NULL byte at the end

	rc, _, lastErr := kext.start.Call(
		uintptr(unsafe.Pointer(&charArray[0])),
	)
	if rc != windows.NO_ERROR {
		return formatErr(lastErr)
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

	rc, _, lastErr := kext.stop.Call()
	if rc != windows.NO_ERROR {
		return formatErr(lastErr)
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

	new := &VerdictRequest{}

	rc, _, lastErr := kext.recvVerdictRequest.Call(
		uintptr(unsafe.Pointer(new)),
	)
	if rc != 0 {
		if rc == 13 /* ERROR_INVALID_DATA */ {
			return nil, nil
		}
		return nil, formatErr(lastErr)
	}
	return new, nil
}

// SetVerdict sets the verdict for a packet and/or connection.
func SetVerdict(packetID uint32, verdict network.Verdict) error {
	kextLock.RLock()
	defer kextLock.RUnlock()
	if !ready.IsSet() {
		return ErrKextNotReady
	}

	rc, _, lastErr := kext.setVerdict.Call(
		uintptr(packetID),
		uintptr(verdict),
	)
	if rc != windows.NO_ERROR {
		return formatErr(lastErr)
	}
	return nil
}

// GetPayload returns the payload of a packet.
func GetPayload(packetID uint32, packetSize uint32) ([]byte, error) {
	kextLock.RLock()
	defer kextLock.RUnlock()
	if !ready.IsSet() {
		return nil, ErrKextNotReady
	}

	buf := make([]byte, packetSize)

	rc, _, lastErr := kext.getPayload.Call(
		uintptr(packetID),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&packetSize)),
	)
	if rc != windows.NO_ERROR {
		return nil, formatErr(lastErr)
	}

	if packetSize == 0 {
		return nil, errors.New("windows kext did not return any data")
	}

	if packetSize < uint32(len(buf)) {
		return buf[:packetSize], nil
	}
	return buf, nil
}

func formatErr(err error) error {
	sysErr, ok := err.(syscall.Errno)
	if ok {
		return fmt.Errorf("%s [0x%X]", err, uintptr(sysErr))
	}
	return err
}
