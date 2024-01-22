package winlogdns

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procOpenEventLog  = advapi32.NewProc("OpenEventLogW")
	procCloseEventLog = advapi32.NewProc("CloseEventLog")
	procReadEventLog  = advapi32.NewProc("ReadEventLogW")
)

type DNSLogReader struct {
	handle uintptr
}

type Win32_NTLogEvent struct {
	Logfile     string
	RecordID    uint32
	EventCode   uint16
	ProcessName string // Process name that generated the event
	ProcessID   uint32 // Process ID (PID) of the generating process
	Message     string // Event message containing DNS query and response details
}

func NewDNSLogListener() (*DNSLogReader, error) {
	// Open event log.
	lr := new(DNSLogReader)
	if err := lr.openEventLog(); err != nil {
		return nil, err
	}

	return lr, nil
}

func (lr *DNSLogReader) openEventLog() error {
	// Convert strings.
	host, err := syscall.UTF16PtrFromString("")
	if err != nil {
		return err
	}
	source, err := syscall.UTF16PtrFromString("DNS Client Events")
	if err != nil {
		return err
	}

	// Open event log for DNS client events.
	handle, _, err := procOpenEventLog.Call(
		uintptr(unsafe.Pointer(host)),
		uintptr(unsafe.Pointer(source)),
	)
	if err != nil {
		return err
	}

	// Set handle and return
	lr.handle = handle
	return nil
}

func (lr *DNSLogReader) readEventLog(readflags, recordoffset uint32, buffer []byte, numberofbytestoread uint32, bytesread, minnumberofbytesneeded *uint32) (*Win32_NTLogEvent, error) {
	ret, _, err := procReadEventLog.Call(
		uintptr(lr.handle),
		uintptr(readflags),
		uintptr(recordoffset),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(numberofbytestoread),
		uintptr(unsafe.Pointer(bytesread)),
		uintptr(unsafe.Pointer(minnumberofbytesneeded)))
	if err != nil {
		return nil, err
	}
	if ret != 0 {
		return nil, fmt.Errorf("failed with return code %d", ret)
	}

	// What do I do here?

	return nil, nil
}

func (lr *DNSLogReader) Close() error {
	ret, _, err := procCloseEventLog.Call(lr.handle)
	if err != nil {
		return err
	}
	if ret != 0 {
		return fmt.Errorf("failed with return code %d", ret)
	}

	return nil
}
