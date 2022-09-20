//go:build windows
// +build windows

package windowskext

import (
	"errors"
	"fmt"
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

	kext           *WinKext
	kextLock       sync.RWMutex
	ready          = abool.NewBool(false)
	urgentRequests *int32
)

func init() {
	var urgentRequestsValue int32
	urgentRequests = &urgentRequestsValue
}

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
	clearCache         *windows.Proc
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
	new.clearCache, err = new.dll.FindProc("PortmasterClearCache")
	if err != nil {
		// the loaded dll is an old version
		log.Errorf("could not find proc PortmasterClearCache (v1.0.12+) in dll: %s", err)
	}

	// initialize dll/kext
	rc, _, lastErr := new.init.Call()
	if rc != windows.NO_ERROR {
		return formatErr(lastErr, rc)
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
		return formatErr(lastErr, rc)
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
		return formatErr(lastErr, rc)
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

	// timestamp := time.Now()
	rc, _, lastErr := kext.recvVerdictRequest.Call(
		uintptr(unsafe.Pointer(new)),
	)
	// log.Tracef("winkext: getting verdict request took %s", time.Now().Sub(timestamp))

	if rc != windows.NO_ERROR {
		if rc == winErrInvalidData {
			return nil, nil
		}
		return nil, formatErr(lastErr, rc)
	}
	return new, nil
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

	atomic.AddInt32(urgentRequests, 1)
	// timestamp := time.Now()
	rc, _, lastErr := kext.setVerdict.Call(
		uintptr(pkt.verdictRequest.id),
		uintptr(verdict),
	)
	// log.Tracef("winkext: settings verdict for packetID %d took %s", packetID, time.Now().Sub(timestamp))
	atomic.AddInt32(urgentRequests, -1)
	if rc != windows.NO_ERROR {
		log.Tracer(pkt.Ctx()).Errorf("kext: failed to set verdict %s on packet %d", verdict, pkt.verdictRequest.id)
		return formatErr(lastErr, rc)
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

	atomic.AddInt32(urgentRequests, 1)
	// timestamp := time.Now()
	rc, _, lastErr := kext.getPayload.Call(
		uintptr(packetID),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&packetSize)),
	)
	// log.Tracef("winkext: getting payload for packetID %d took %s", packetID, time.Now().Sub(timestamp))
	atomic.AddInt32(urgentRequests, -1)

	if rc != windows.NO_ERROR {
		return nil, formatErr(lastErr, rc)
	}

	if packetSize == 0 {
		return nil, errors.New("windows kext did not return any data")
	}

	if packetSize < uint32(len(buf)) {
		return buf[:packetSize], nil
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

	if kext.clearCache == nil {
		log.Error("kext: cannot clear cache: clearCache function  missing")
	}

	rc, _, lastErr := kext.clearCache.Call()

	if rc != windows.NO_ERROR {
		return formatErr(lastErr, rc)
	}

	return nil
}

func formatErr(err error, rc uintptr) error {
	sysErr, ok := err.(syscall.Errno)
	if ok {
		return fmt.Errorf("%s [LE 0x%X] [RC 0x%X]", err, uintptr(sysErr), rc)
	}
	return err
}
