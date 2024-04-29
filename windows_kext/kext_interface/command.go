package kext_interface

import (
	"encoding/binary"
	"io"
)

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

type KextVerdict uint8

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
	Id      uint64
	Verdict uint8
}

type RedirectV4 struct {
	command       uint8
	Id            uint64
	RemoteAddress [4]byte
	RemotePort    uint16
}

type RedirectV6 struct {
	command       uint8
	Id            uint64
	RemoteAddress [16]byte
	RemotePort    uint16
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

func SendShutdownCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandShutdown})
	return err
}

func SendVerdictCommand(writer io.Writer, verdict Verdict) error {
	verdict.command = CommandVerdict
	return binary.Write(writer, binary.LittleEndian, verdict)
}

func SendUpdateV4Command(writer io.Writer, update UpdateV4) error {
	update.command = CommandUpdateV4
	return binary.Write(writer, binary.LittleEndian, update)
}

func SendUpdateV6Command(writer io.Writer, update UpdateV6) error {
	update.command = CommandUpdateV6
	return binary.Write(writer, binary.LittleEndian, update)
}

func SendClearCacheCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandClearCache})
	return err
}

func SendGetLogsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandGetLogs})
	return err
}

func SendGetBandwidthStatsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandBandwidthStats})
	return err
}

func SendPrintMemoryStatsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandPrintMemoryStats})
	return err
}

func SendCleanEndedConnectionsCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandCleanEndedConnections})
	return err
}
