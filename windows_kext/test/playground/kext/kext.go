//go:build windows
// +build windows

package kext

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	stopServiceTimeoutDuration = 30 * time.Second
	defaultDriverName          = "PortmasterKext"
)

// Command IDs
const (
	CommandShutdown              = 0
	CommandVerdict               = 1
	CommandUpdateV4              = 2
	CommandUpdateV6              = 3
	CommandClearCache            = 4
	CommandGetLogs               = 5
	CommandBandwidthStats        = 6
	CommandPrintMemoryStats      = 7
	CommandCleanEndedConnections = 8
)

// KextVerdict is the verdict ID used with the kext.
type KextVerdict uint8

// Kext Verdicts - must be in sync with Rust driver
const (
	VerdictUndecided           KextVerdict = 0
	VerdictUndeterminable      KextVerdict = 1
	VerdictAccept              KextVerdict = 2
	VerdictPermanentAccept     KextVerdict = 3
	VerdictBlock               KextVerdict = 4
	VerdictPermanentBlock      KextVerdict = 5
	VerdictDrop                KextVerdict = 6
	VerdictPermanentDrop       KextVerdict = 7
	VerdictRerouteToNameserver KextVerdict = 8
	VerdictRerouteToTunnel     KextVerdict = 9
	VerdictFailed              KextVerdict = 10
)

func (v KextVerdict) String() string {
	switch v {
	case VerdictUndecided:
		return "Undecided"
	case VerdictUndeterminable:
		return "Undeterminable"
	case VerdictAccept:
		return "Accept"
	case VerdictPermanentAccept:
		return "PermanentAccept"
	case VerdictBlock:
		return "Block"
	case VerdictPermanentBlock:
		return "PermanentBlock"
	case VerdictDrop:
		return "Drop"
	case VerdictPermanentDrop:
		return "PermanentDrop"
	case VerdictRerouteToNameserver:
		return "RerouteToNameserver"
	case VerdictRerouteToTunnel:
		return "RerouteToTunnel"
	case VerdictFailed:
		return "Failed"
	default:
		return fmt.Sprintf("Unknown(%d)", v)
	}
}

// Info types from driver
const (
	InfoLogLine              = 0
	InfoConnectionIpv4       = 1
	InfoConnectionIpv6       = 2
	InfoConnectionEndEventV4 = 3
	InfoConnectionEndEventV6 = 4
	InfoBandwidthStatsV4     = 5
	InfoBandwidthStatsV6     = 6
)

var (
	ErrUnknownInfoType     = errors.New("unknown info type")
	ErrUnexpectedInfoSize  = errors.New("unexpected info size")
	ErrUnexpectedReadError = errors.New("unexpected read error")
	ErrServiceNotValid     = errors.New("kext service not initialized")
	ErrFileNotValid        = errors.New("kext file not valid")
)

// Verdict command structure
type Verdict struct {
	Command uint8
	ID      uint64
	Verdict uint8
}

// UpdateV4 command structure
type UpdateV4 struct {
	Command       uint8
	Protocol      uint8
	LocalAddress  [4]byte
	LocalPort     uint16
	RemoteAddress [4]byte
	RemotePort    uint16
	Verdict       uint8
}

// UpdateV6 command structure
type UpdateV6 struct {
	Command       uint8
	Protocol      uint8
	LocalAddress  [16]byte
	LocalPort     uint16
	RemoteAddress [16]byte
	RemotePort    uint16
	Verdict       uint8
}

// ConnectionV4 received from driver
type ConnectionV4 struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      [4]byte
	RemoteIP     [4]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
	Payload      []byte
}

// ConnectionV6 received from driver
type ConnectionV6 struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      [16]byte
	RemoteIP     [16]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
	Payload      []byte
}

// ConnectionEndV4 received from driver
type ConnectionEndV4 struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [4]byte
	RemoteIP   [4]byte
	LocalPort  uint16
	RemotePort uint16
}

// ConnectionEndV6 received from driver
type ConnectionEndV6 struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [16]byte
	RemoteIP   [16]byte
	LocalPort  uint16
	RemotePort uint16
}

// LogLine received from driver
type LogLine struct {
	Severity byte
	Line     string
}

// Log severity levels
const (
	SeverityTrace    = 1
	SeverityDebug    = 2
	SeverityInfo     = 3
	SeverityWarning  = 4
	SeverityError    = 5
	SeverityCritical = 6
)

func SeverityString(s byte) string {
	switch s {
	case SeverityTrace:
		return "TRACE"
	case SeverityDebug:
		return "DEBUG"
	case SeverityInfo:
		return "INFO"
	case SeverityWarning:
		return "WARN"
	case SeverityError:
		return "ERROR"
	case SeverityCritical:
		return "CRIT"
	default:
		return fmt.Sprintf("LVL%d", s)
	}
}

// Info represents a parsed info packet from driver
type Info struct {
	ConnectionV4    *ConnectionV4
	ConnectionV6    *ConnectionV6
	ConnectionEndV4 *ConnectionEndV4
	ConnectionEndV6 *ConnectionEndV6
	LogLine         *LogLine
}

// KextService manages the kernel driver service
type KextService struct {
	handle     windows.Handle
	driverName string
}

// KextFile handles communication with the driver
type KextFile struct {
	handle    windows.Handle
	buffer    []byte
	readSlice []byte
}

// NewKextService creates or opens the kernel driver service
func NewKextService(driverName string, driverPath string) (*KextService, error) {
	if driverName == "" {
		driverName = defaultDriverName
	}

	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS)
	if err != nil {
		return nil, fmt.Errorf("failed to open service manager: %w", err)
	}
	defer windows.CloseServiceHandle(manager)

	driverNameU16, err := syscall.UTF16FromString(driverName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver name to UTF16: %w", err)
	}

	// Check if there is an existing service
	service, err := windows.OpenService(manager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
	if err == nil {
		// Old service found - stop and delete it
		fmt.Println("[kext] Old driver service found, cleaning up...")
		oldService := &KextService{handle: service, driverName: driverName}
		if err := oldService.Stop(true); err != nil {
			return nil, fmt.Errorf("failed to stop old service: %w", err)
		}
		if err := oldService.Delete(); err != nil {
			// Ignore "marked for deletion" error - service will be cleaned up
			if !strings.Contains(err.Error(), "marked for deletion") {
				return nil, fmt.Errorf("failed to delete old service: %w", err)
			}
			fmt.Println("[kext] Service marked for deletion, waiting...")
		}
		if err := oldService.WaitUntilDeleted(manager); err != nil {
			return nil, fmt.Errorf("failed waiting for old service deletion: %w", err)
		}
		fmt.Println("[kext] Old driver service deleted successfully")
	}

	driverPathU16, err := syscall.UTF16FromString(driverPath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver path to UTF16: %w", err)
	}

	// Create the service
	service, err = windows.CreateService(
		manager,
		&driverNameU16[0],
		&driverNameU16[0],
		windows.SERVICE_ALL_ACCESS,
		windows.SERVICE_KERNEL_DRIVER,
		windows.SERVICE_DEMAND_START,
		windows.SERVICE_ERROR_NORMAL,
		&driverPathU16[0],
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return &KextService{handle: service, driverName: driverName}, nil
}

func (s *KextService) isValid() bool {
	return s != nil && s.handle != windows.InvalidHandle && s.handle != 0
}

// IsRunning checks if the service is currently running
func (s *KextService) IsRunning() (bool, error) {
	if !s.isValid() {
		return false, ErrServiceNotValid
	}
	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(s.handle, &status); err != nil {
		return false, err
	}
	return status.CurrentState == windows.SERVICE_RUNNING, nil
}

func (s *KextService) waitForStatus(neededStatus uint32, timeout time.Duration) error {
	var status windows.SERVICE_STATUS
	start := time.Now()
	for {
		if err := windows.QueryServiceStatus(s.handle, &status); err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
		if status.CurrentState == neededStatus {
			return nil
		}
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for service status %d", neededStatus)
		}
		time.Sleep(time.Duration(status.WaitHint/10) * time.Millisecond)
	}
}

// Start starts the driver service
func (s *KextService) Start(wait bool) error {
	if !s.isValid() {
		return ErrServiceNotValid
	}

	if err := windows.StartService(s.handle, 0, nil); err != nil {
		if err != windows.ERROR_SERVICE_ALREADY_RUNNING {
			return fmt.Errorf("failed to start service: %w", err)
		}
	}

	if wait {
		if err := s.waitForStatus(windows.SERVICE_RUNNING, stopServiceTimeoutDuration); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops the driver service
func (s *KextService) Stop(wait bool) error {
	fmt.Println("[kext] Stopping driver service...")
	if !s.isValid() {
		return ErrServiceNotValid
	}

	fmt.Println("[kext] Sending stop control to driver service...")
	var status windows.SERVICE_STATUS
	if err := windows.ControlService(s.handle, windows.SERVICE_CONTROL_STOP, &status); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	if wait {
		fmt.Println("[kext] Waiting for driver service to stop...")
		if err := s.waitForStatus(windows.SERVICE_STOPPED, 10*time.Second); err != nil {
			return err
		}
	}
	fmt.Println("[kext] Driver service stopped successfully")
	return nil
}

// Delete deletes the driver service
func (s *KextService) Delete() error {
	if !s.isValid() {
		return ErrServiceNotValid
	}

	if err := windows.DeleteService(s.handle); err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	if err := windows.CloseServiceHandle(s.handle); err != nil {
		return fmt.Errorf("failed to close service handle: %w", err)
	}

	s.handle = windows.InvalidHandle
	return nil
}

// WaitUntilDeleted waits until the service is fully deleted
func (s *KextService) WaitUntilDeleted(manager windows.Handle) error {
	driverNameU16, err := syscall.UTF16FromString(s.driverName)
	if err != nil {
		return fmt.Errorf("failed to convert driver name: %w", err)
	}

	timeout := 30 * time.Second
	start := time.Now()
	for {
		handle, err := windows.OpenService(manager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
		if err != nil {
			return nil // Service no longer exists
		}
		_ = windows.CloseServiceHandle(handle)

		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for service deletion")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// OpenFile opens a communication channel with the driver
func (s *KextService) OpenFile(readBufferSize int) (*KextFile, error) {
	if !s.isValid() {
		return nil, ErrServiceNotValid
	}

	devicePath := `\\.\` + s.driverName
	devicePathU16, err := syscall.UTF16FromString(devicePath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert device path: %w", err)
	}

	handle, err := windows.CreateFile(
		&devicePathU16[0],
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	return &KextFile{
		handle: handle,
		buffer: make([]byte, readBufferSize),
	}, nil
}

// Close closes the service handle
func (s *KextService) Close() error {
	if !s.isValid() {
		return nil
	}
	err := windows.CloseServiceHandle(s.handle)
	s.handle = windows.InvalidHandle
	return err
}

// KextFile methods

func (f *KextFile) isValid() bool {
	return f != nil && f.handle != windows.InvalidHandle && f.handle != 0
}

// Read reads data from the driver
func (f *KextFile) Read(buffer []byte) (int, error) {
	if !f.isValid() {
		return 0, ErrFileNotValid
	}

	// If no cached data, read from driver
	if len(f.readSlice) == 0 {
		if err := f.refillBuffer(); err != nil {
			return 0, err
		}
	}

	if len(f.readSlice) >= len(buffer) {
		copy(buffer, f.readSlice[:len(buffer)])
		f.readSlice = f.readSlice[len(buffer):]
		return len(buffer), nil
	}

	// Not enough data - copy what we have and read more
	copied := copy(buffer, f.readSlice)
	f.readSlice = nil
	n, err := f.Read(buffer[copied:])
	return copied + n, err
}

func (f *KextFile) refillBuffer() error {
	var count uint32
	overlapped := &windows.Overlapped{}
	if err := windows.ReadFile(f.handle, f.buffer, &count, overlapped); err != nil {
		return err
	}
	f.readSlice = f.buffer[:count]
	return nil
}

// Write writes data to the driver
func (f *KextFile) Write(buffer []byte) (int, error) {
	if !f.isValid() {
		return 0, ErrFileNotValid
	}
	var count uint32
	overlapped := &windows.Overlapped{}
	if err := windows.WriteFile(f.handle, buffer, &count, overlapped); err != nil {
		return 0, err
	}
	return int(count), nil
}

// Close closes the driver file handle
func (f *KextFile) Close() error {
	if !f.isValid() {
		return nil
	}
	err := windows.CloseHandle(f.handle)
	f.handle = windows.InvalidHandle
	return err
}

// Command functions

// SendShutdownCommand sends shutdown command to driver
func SendShutdownCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandShutdown})
	return err
}

// SendVerdictCommand sends a verdict for a connection
func SendVerdictCommand(w io.Writer, id uint64, verdict KextVerdict) error {
	v := Verdict{
		Command: CommandVerdict,
		ID:      id,
		Verdict: uint8(verdict),
	}
	return binary.Write(w, binary.LittleEndian, v)
}

// SendUpdateV4Command sends an IPv4 verdict update
func SendUpdateV4Command(w io.Writer, update UpdateV4) error {
	update.Command = CommandUpdateV4
	return binary.Write(w, binary.LittleEndian, update)
}

// SendUpdateV6Command sends an IPv6 verdict update
func SendUpdateV6Command(w io.Writer, update UpdateV6) error {
	update.Command = CommandUpdateV6
	return binary.Write(w, binary.LittleEndian, update)
}

// SendClearCacheCommand clears the driver cache
func SendClearCacheCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandClearCache})
	return err
}

// SendGetLogsCommand requests buffered logs from driver
func SendGetLogsCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandGetLogs})
	return err
}

// SendGetBandwidthStatsCommand requests bandwidth statistics
func SendGetBandwidthStatsCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandBandwidthStats})
	return err
}

// Info parsing

type readHelper struct {
	infoType    byte
	commandSize uint32
	readSize    int
	reader      io.Reader
}

func newReadHelper(r io.Reader) (*readHelper, error) {
	h := &readHelper{reader: r}
	if err := binary.Read(r, binary.LittleEndian, &h.infoType); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &h.commandSize); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *readHelper) Read(p []byte) (int, error) {
	n, err := h.reader.Read(p)
	h.readSize += n
	return n, err
}

func (h *readHelper) readData(data any) error {
	if err := binary.Read(h, binary.LittleEndian, data); err != nil {
		return errors.Join(ErrUnexpectedReadError, err)
	}
	if uint32(h.readSize) > h.commandSize {
		return ErrUnexpectedInfoSize
	}
	return nil
}

func (h *readHelper) readBytes(size uint32) ([]byte, error) {
	if uint32(h.readSize) >= h.commandSize {
		return nil, errors.Join(fmt.Errorf("read past end"), ErrUnexpectedReadError)
	}
	if size == 0 {
		size = h.commandSize - uint32(h.readSize)
	}
	if h.commandSize < uint32(h.readSize)+size {
		return nil, ErrUnexpectedInfoSize
	}
	buf := make([]byte, size)
	if err := binary.Read(h, binary.LittleEndian, buf); err != nil {
		return nil, errors.Join(ErrUnexpectedReadError, err)
	}
	return buf, nil
}

func (h *readHelper) readUntilEnd() {
	_, _ = h.readBytes(0)
}

// RecvInfo reads and parses an info packet from the driver
func RecvInfo(r io.Reader) (*Info, error) {
	h, err := newReadHelper(r)
	if err != nil {
		return nil, err
	}
	defer h.readUntilEnd()

	switch h.infoType {
	case InfoConnectionIpv4:
		return parseConnectionV4(h)
	case InfoConnectionIpv6:
		return parseConnectionV6(h)
	case InfoConnectionEndEventV4:
		return parseConnectionEndV4(h)
	case InfoConnectionEndEventV6:
		return parseConnectionEndV6(h)
	case InfoLogLine:
		return parseLogLine(h)
	case InfoBandwidthStatsV4, InfoBandwidthStatsV6:
		// Skip bandwidth stats for now
		return nil, nil
	}
	return nil, ErrUnknownInfoType
}

func parseConnectionV4(h *readHelper) (*Info, error) {
	var fixed struct {
		ID           uint64
		ProcessID    uint64
		Direction    byte
		Protocol     byte
		LocalIP      [4]byte
		RemoteIP     [4]byte
		LocalPort    uint16
		RemotePort   uint16
		PayloadLayer uint8
	}
	if err := h.readData(&fixed); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV4: %w", err)
	}

	var payloadSize uint32
	if err := h.readData(&payloadSize); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV4 payload size: %w", err)
	}

	var payload []byte
	if payloadSize > 0 {
		var err error
		payload, err = h.readBytes(payloadSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ConnectionV4 payload: %w", err)
		}
	}

	return &Info{
		ConnectionV4: &ConnectionV4{
			ID:           fixed.ID,
			ProcessID:    fixed.ProcessID,
			Direction:    fixed.Direction,
			Protocol:     fixed.Protocol,
			LocalIP:      fixed.LocalIP,
			RemoteIP:     fixed.RemoteIP,
			LocalPort:    fixed.LocalPort,
			RemotePort:   fixed.RemotePort,
			PayloadLayer: fixed.PayloadLayer,
			Payload:      payload,
		},
	}, nil
}

func parseConnectionV6(h *readHelper) (*Info, error) {
	var fixed struct {
		ID           uint64
		ProcessID    uint64
		Direction    byte
		Protocol     byte
		LocalIP      [16]byte
		RemoteIP     [16]byte
		LocalPort    uint16
		RemotePort   uint16
		PayloadLayer uint8
	}
	if err := h.readData(&fixed); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV6: %w", err)
	}

	var payloadSize uint32
	if err := h.readData(&payloadSize); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV6 payload size: %w", err)
	}

	var payload []byte
	if payloadSize > 0 {
		var err error
		payload, err = h.readBytes(payloadSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ConnectionV6 payload: %w", err)
		}
	}

	return &Info{
		ConnectionV6: &ConnectionV6{
			ID:           fixed.ID,
			ProcessID:    fixed.ProcessID,
			Direction:    fixed.Direction,
			Protocol:     fixed.Protocol,
			LocalIP:      fixed.LocalIP,
			RemoteIP:     fixed.RemoteIP,
			LocalPort:    fixed.LocalPort,
			RemotePort:   fixed.RemotePort,
			PayloadLayer: fixed.PayloadLayer,
			Payload:      payload,
		},
	}, nil
}

func parseConnectionEndV4(h *readHelper) (*Info, error) {
	var conn ConnectionEndV4
	if err := h.readData(&conn); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionEndV4: %w", err)
	}
	return &Info{ConnectionEndV4: &conn}, nil
}

func parseConnectionEndV6(h *readHelper) (*Info, error) {
	var conn ConnectionEndV6
	if err := h.readData(&conn); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionEndV6: %w", err)
	}
	return &Info{ConnectionEndV6: &conn}, nil
}

func parseLogLine(h *readHelper) (*Info, error) {
	var severity byte
	if err := h.readData(&severity); err != nil {
		return nil, fmt.Errorf("failed to parse LogLine severity: %w", err)
	}
	lineBytes, err := h.readBytes(0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LogLine text: %w", err)
	}
	return &Info{
		LogLine: &LogLine{
			Severity: severity,
			Line:     string(lineBytes),
		},
	}, nil
}
