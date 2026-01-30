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

	// Split tunneling related commands

	// Enables split tunneling functionality.
	//
	// When enabled, the driver will:
	// - Send BindRequest notifications for new connections
	// - Allow SplitTunnel commands to modify connection routing
	CommandEnableSplitTunnel = 9
	// Disables split tunneling functionality.
	//
	// When disabled, the driver will:
	// - Stop sending BindRequest notifications
	// - SplitTunnel commands will not have any effect.
	CommandDisableSplitTunnel = 10
	// Response to a bind redirect request (Split-Tunneling verdict).
	//
	// This command is sent from user-space to the driver in response to
	// BindRequest notifications. It tells the driver whether to:
	// - Allow the original bind operation (no redirect)
	// - Redirect the bind to a specific local IP address
	CommandSplitTunnel = 11
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

// RedirectV4 command structure - response to RedirectionRequestV4
type SplitTunnel struct {
	Command uint8
	ID      uint64
	// IPv4 local address to bind to.
	// - Unspecified (0.0.0.0) - Allow original bind without redirect
	// - Specific address - Redirect bind to this IPv4 address
	LocalAddress_IPv4 [4]byte // Local interface IP to redirect to (when Redirect = 1)
	// IPv6 local address to bind to.
	// - Unspecified (::) - Allow original bind without redirect
	// - Specific address - Redirect bind to this IPv6 address
	LocalAddress_IPv6 [16]byte
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

// SendEnableSplitTunnelCommand enables split tunneling in the driver
func SendEnableSplitTunnelCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandEnableSplitTunnel})
	return err
}

// SendDisableSplitTunnelCommand disables split tunneling in the driver
func SendDisableSplitTunnelCommand(writer io.Writer) error {
	_, err := writer.Write([]byte{CommandDisableSplitTunnel})
	return err
}

// SendRedirectV4Command sends a redirect (split-tunnel) decision for a connection
func SendSplitTunnelCommand(writer io.Writer, redirect SplitTunnel) error {
	redirect.Command = CommandSplitTunnel
	return binary.Write(writer, binary.LittleEndian, redirect)
}
