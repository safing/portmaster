//go:build windows
// +build windows

// Package kext provides communication with the Portmaster kernel extension driver.
package kext

import (
	"errors"
	"fmt"
	"time"
)

const (
	stopServiceTimeoutDuration = 30 * time.Second
	defaultDriverName          = "PortmasterKext"
)

// Command IDs - must be in sync with Rust driver
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
	CommandRedirectV4            = 9
	CommandRedirectV6            = 10
	CommandEnableSplitTunnel     = 11
	CommandDisableSplitTunnel    = 12
)

// Info types from driver
const (
	InfoLogLine              = 0
	InfoConnectionIpv4       = 1
	InfoConnectionIpv6       = 2
	InfoConnectionEndEventV4 = 3
	InfoConnectionEndEventV6 = 4
	InfoBandwidthStatsV4     = 5
	InfoBandwidthStatsV6     = 6
	InfoRedirectionRequestV4 = 7
	InfoRedirectionRequestV6 = 8
)

// Log severity levels
const (
	SeverityTrace    = 1
	SeverityDebug    = 2
	SeverityInfo     = 3
	SeverityWarning  = 4
	SeverityError    = 5
	SeverityCritical = 6
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

// Errors
var (
	ErrUnknownInfoType     = errors.New("unknown info type")
	ErrUnexpectedInfoSize  = errors.New("unexpected info size")
	ErrUnexpectedReadError = errors.New("unexpected read error")
	ErrServiceNotValid     = errors.New("kext service not initialized")
	ErrFileNotValid        = errors.New("kext file not valid")
)
