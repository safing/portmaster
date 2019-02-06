package windowskext

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/tevino/abool"
)

type WinKext struct {
	dll *windows.DLL

	recvVerdictRequest *windows.Proc

	valid *abool.AtomicBool
}

type VerdictRequest struct {
	ID        uint32
	ProcessID uint32
	Direction bool
	IPv6      bool
	SrcIP     [4]uint32
	DstIP     [4]uint32
	SrcPort   uint16
	DstPort   uint16
	Protocol  uint8
}

func New(dllLocation string) (*WinKext, error) {

	new := &WinKext{}
	var err error

	// load dll
	new.dll, err = windows.LoadDLL(dllLocation)
	if err != nil {
		return nil, err
	}

	// load functions
	new.recvVerdictRequest, err = new.dll.FindProc("PortmasterRecvVerdictRequest")
	if err != nil {
		return nil, fmt.Errorf("could not find proc PortmasterRecvVerdictRequest: %s", err)
	}

	return new, nil
}

func (kext *WinKext) RecvVerdictRequest() (*VerdictRequest, error) {
	new := &VerdictRequest{}

	rc, _, lastErr := kext.recvVerdictRequest.Call(
		uintptr(unsafe.Pointer(new)),
	)
	if rc != 0 {
		return nil, lastErr
	}
	return new, nil
}
