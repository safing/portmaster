package windivert

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/tevino/abool"
)

type WinDivert struct {
	dll    *windows.DLL
	handle uintptr

	open                *windows.Proc
	recv                *windows.Proc
	send                *windows.Proc
	close               *windows.Proc
	setParam            *windows.Proc
	getParam            *windows.Proc
	helperCalcChecksums *windows.Proc
	helperCheckFilter   *windows.Proc

	valid *abool.AtomicBool
}

// copied from windivert.h
type WinDivertAddress struct {
	Timestamp         int64  /* Packet's timestamp. */
	IfIdx             uint32 /* Packet's interface index. */
	SubIfIdx          uint32 /* Packet's sub-interface index. */
	Direction         uint8  /* Packet's direction. */
	Loopback          uint8  /* Packet is loopback? */
	Impostor          uint8  /* Packet is impostor? */
	PseudoIPChecksum  uint8  /* Packet has pseudo IPv4 checksum? */
	PseudoTCPChecksum uint8  /* Packet has pseudo TCP checksum? */
	PseudoUDPChecksum uint8  /* Packet has pseudo UDP checksum? */
	Reserved          uint8
}

// copied from windivert.h
const (
	directionInbound  uint8 = 1
	directionOutbound uint8 = 0

	// Divert layers
	layerNetwork        uintptr = 0 /* Network layer. */
	layerNetworkForward uintptr = 1 /* Network layer (forwarded packets) */

	// Divert parameters
	flagSniff uintptr = 1
	flagDrop  uintptr = 2
	flagDebug uintptr = 4

	paramQueueLen  uintptr = 0 /* Packet queue length. */
	paramQueueTime uintptr = 1 /* Packet queue time. */
	paramQueueSize uintptr = 2 /* Packet queue size. */

	rvInvalidHandle int     = -1
	rvFalse         uintptr = 0
	rvTrue          uintptr = 1
)

func New(dllLocation, filter string) (*WinDivert, error) {

	new := &WinDivert{}
	var err error

	// load dll
	new.dll, err = windows.LoadDLL(dllLocation)
	if err != nil {
		return nil, err
	}

	// load functions
	new.open, err = new.dll.FindProc("WinDivertOpen")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertOpen: %s", err)
	}
	new.recv, err = new.dll.FindProc("WinDivertRecv")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertRecv: %s", err)
	}
	new.send, err = new.dll.FindProc("WinDivertSend")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertSend: %s", err)
	}
	new.close, err = new.dll.FindProc("WinDivertClose")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertClose: %s", err)
	}
	new.setParam, err = new.dll.FindProc("WinDivertSetParam")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertSetParam: %s", err)
	}
	new.getParam, err = new.dll.FindProc("WinDivertGetParam")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertGetParam: %s", err)
	}
	new.helperCalcChecksums, err = new.dll.FindProc("WinDivertHelperCalcChecksums")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertHelperCalcChecksums: %s", err)
	}
	new.helperCheckFilter, err = new.dll.FindProc("WinDivertHelperCheckFilter")
	if err != nil {
		return nil, fmt.Errorf("could not find proc WinDivertHelperCheckFilter: %s", err)
	}

	// default filter
	if filter == "" {
		filter = "true"
	}

	// open
	err = new.Open(filter)
	if err != nil {
		return nil, fmt.Errorf("could not open new windivert handle: %s", err)
	}

	return new, nil

}

func (wd *WinDivert) Open(filter string) error {

	r1, _, lastErr := wd.open.Call(
		stringToPtr(filter), // __in        const char *filter
		layerNetwork,        // __in        WINDIVERT_LAYER layer
		0,                   // __in        INT16 priority
		0,                   // __in        UINT64 flags
	)
	if int(r1) == rvInvalidHandle {
		return lastErr
	}

	wd.handle = r1
	wd.valid = abool.NewBool(true)
	return nil
}

func (wd *WinDivert) Recv() ([]byte, *WinDivertAddress, error) {
	buf := make([]byte, 4096) // TODO: we can do this better
	address := &WinDivertAddress{}
	readLen := 0

	r1, _, lastErr := wd.recv.Call(
		wd.handle,                         // __in        HANDLE handle
		byteSliceToPtr(buf),               // __out       PVOID pPacket
		uintptr(len(buf)),                 // __in        UINT packetLen
		uintptr(unsafe.Pointer(address)),  // __out_opt   PWINDIVERT_ADDRESS pAddr
		uintptr(unsafe.Pointer(&readLen)), // __out_opt   UINT *readLen
	)
	if r1 == rvFalse {
		return nil, nil, lastErr
	}
	if readLen == 0 {
		return nil, nil, errors.New("empty read")
	}

	return buf[:readLen], address, nil
}

func (wd *WinDivert) Send(packetData []byte, address *WinDivertAddress) error {
	writeLen := 0

	r1, _, lastErr := wd.send.Call(
		wd.handle,                          // __in        HANDLE handle
		byteSliceToPtr(packetData),         // __in        PVOID pPacket
		uintptr(len(packetData)),           // __in        UINT packetLen
		uintptr(unsafe.Pointer(address)),   // __in        PWINDIVERT_ADDRESS pAddr
		uintptr(unsafe.Pointer(&writeLen)), // __out_opt   UINT *writeLen
	)
	if r1 == rvFalse {
		return lastErr
	}
	return nil
}

func (wd *WinDivert) Close() error {
	r1, _, lastErr := wd.close.Call(
		wd.handle, // __in        HANDLE handle
	)
	if r1 == rvFalse {
		return lastErr
	}
	return nil
}

func (wd *WinDivert) SetParam(param, value uintptr) error {
	r1, _, lastErr := wd.setParam.Call(
		wd.handle, // __in        HANDLE handle
		param,     // __in        WINDIVERT_PARAM param
		value,     // __in        UINT64 value
	)
	if r1 == rvFalse {
		return lastErr
	}
	return nil
}

func (wd *WinDivert) GetParam(param uintptr) (uint64, error) {
	var value uint64

	r1, _, lastErr := wd.getParam.Call(
		wd.handle, // __in        HANDLE handle
		param,     // __in        WINDIVERT_PARAM param
		uintptr(unsafe.Pointer(&value)), // __out       UINT64 *pValue
	)
	if r1 == rvFalse {
		return 0, lastErr
	}
	return value, nil
}

func (wd *WinDivert) HelperCalcChecksums(packetData []byte, address *WinDivertAddress, flags uintptr) error {
	r1, _, lastErr := wd.setParam.Call(
		byteSliceToPtr(packetData),       // __inout     PVOID pPacket
		uintptr(len(packetData)),         // __in        UINT packetLen
		uintptr(unsafe.Pointer(address)), // __in_opt    PWINDIVERT_ADDRESS pAddr
		flags, // __in        UINT64 flags
	)
	if r1 == rvFalse {
		return lastErr
	}
	return nil
}

// func (wd *WinDivert) HelperCheckFilter() {
// 	// __in        const char *filter
// 	// __in        WINDIVERT_LAYER layer
// 	// __out_opt   const char **errorStr
// 	// __out_opt   UINT *errorPos
// }

func stringToPtr(s string) uintptr {
	if !strings.HasSuffix(s, "\x00") {
		s = s + "\x00"
	}
	a := []byte(s)
	return uintptr(unsafe.Pointer(&a[0]))
}

func byteSliceToPtr(a []byte) uintptr {
	return uintptr(unsafe.Pointer(&a[0]))
}
