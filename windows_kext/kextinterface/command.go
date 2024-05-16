package kextinterface

import (
	"encoding/binary"
	"io"
)

// Command IDs.
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

// KextVerdict is the verdict ID used to with the kext.
type KextVerdict uint8

// Kext Verdicts.
// Make sure this is in sync with the Rust version.
const (
	// VerdictUndecided is the default status of new connections.
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

type Verdict struct {
	command uint8
	ID      uint64
	Verdict uint8
}

type UpdateV4 struct {
	command       uint8
	Protocol      uint8
	LocalAddress  [4]byte
	LocalPort     uint16
	RemoteAddress [4]byte
	RemotePort    uint16
	Verdict       uint8
}

type UpdateV6 struct {
	command       uint8
	Protocol      uint8
	LocalAddress  [16]byte
	LocalPort     uint16
	RemoteAddress [16]byte
	RemotePort    uint16
	Verdict       uint8
}

// SendShutdownCommand sends a Shutdown command to the kext.
func SendShutdownCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandShutdown})
	return err
}

// SendVerdictCommand sends a Verdict command to the kext.
func SendVerdictCommand(writer io.Writer, verdict Verdict) error {
	verdict.command = CommandVerdict
	return binary.Write(writer, binary.LittleEndian, verdict)
}

// SendUpdateV4Command sends a UpdateV4 command to the kext.
func SendUpdateV4Command(writer io.Writer, update UpdateV4) error {
	update.command = CommandUpdateV4
	return binary.Write(writer, binary.LittleEndian, update)
}

// SendUpdateV6Command sends a UpdateV6 command to the kext.
func SendUpdateV6Command(writer io.Writer, update UpdateV6) error {
	update.command = CommandUpdateV6
	return binary.Write(writer, binary.LittleEndian, update)
}

// SendClearCacheCommand sends a ClearCache command to the kext.
func SendClearCacheCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandClearCache})
	return err
}

// SendGetLogsCommand sends a GetLogs command to the kext.
func SendGetLogsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandGetLogs})
	return err
}

// SendGetBandwidthStatsCommand sends a GetBandwidthStats command to the kext.
func SendGetBandwidthStatsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandBandwidthStats})
	return err
}

// SendPrintMemoryStatsCommand sends a PrintMemoryStats command to the kext.
func SendPrintMemoryStatsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandPrintMemoryStats})
	return err
}

// SendCleanEndedConnectionsCommand sends a CleanEndedConnections command to the kext.
func SendCleanEndedConnectionsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandCleanEndedConnections})
	return err
}
