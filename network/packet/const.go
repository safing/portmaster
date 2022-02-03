package packet

import (
	"errors"
	"fmt"
)

// Basic Types.
type (
	// IPVersion represents an IP version.
	IPVersion uint8
	// IPProtocol represents an IP protocol.
	IPProtocol uint8
	// Verdict describes the decision on a packet.
	Verdict uint8
)

// Basic Constants.
const (
	IPv4 = IPVersion(4)
	IPv6 = IPVersion(6)

	InBound  = true
	OutBound = false

	ICMP    = IPProtocol(1)
	IGMP    = IPProtocol(2)
	TCP     = IPProtocol(6)
	UDP     = IPProtocol(17)
	ICMPv6  = IPProtocol(58)
	UDPLite = IPProtocol(136)
	RAW     = IPProtocol(255)

	AnyHostInternalProtocol61 = IPProtocol(61)
)

// Verdicts.
const (
	DROP Verdict = iota
	BLOCK
	ACCEPT
	STOLEN
	QUEUE
	REPEAT
	STOP
)

// ErrFailedToLoadPayload is returned by GetPayload if it failed for an unspecified reason, or is not implemented on the current system.
var ErrFailedToLoadPayload = errors.New("could not load packet payload")

// ByteSize returns the byte size of the ip (IPv4 = 4 bytes, IPv6 = 16).
func (v IPVersion) ByteSize() int {
	switch v {
	case IPv4:
		return 4
	case IPv6:
		return 16
	}
	return 0
}

// String returns the string representation of the IP version: "IPv4" or "IPv6".
func (v IPVersion) String() string {
	switch v {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	}
	return fmt.Sprintf("<unknown ip version, %d>", uint8(v))
}

// String returns the string representation (abbreviation) of the protocol.
func (p IPProtocol) String() string {
	switch p {
	case RAW:
		return "RAW"
	case TCP:
		return "TCP"
	case UDP:
		return "UDP"
	case UDPLite:
		return "UDPLite"
	case ICMP:
		return "ICMP"
	case ICMPv6:
		return "ICMPv6"
	case IGMP:
		return "IGMP"
	case AnyHostInternalProtocol61:
		fallthrough
	default:
		return fmt.Sprintf("<unknown protocol, %d>", uint8(p))
	}
}

// String returns the string representation of the verdict.
func (v Verdict) String() string {
	switch v {
	case DROP:
		return "DROP"
	case BLOCK:
		return "BLOCK"
	case ACCEPT:
		return "ACCEPT"
	case STOLEN:
		return "STOLEN"
	case QUEUE:
		return "QUEUE"
	case REPEAT:
		return "REPEAT"
	case STOP:
		return "STOP"
	default:
		return fmt.Sprintf("<unsupported verdict, %d>", uint8(v))
	}
}
