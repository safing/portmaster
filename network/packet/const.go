package packet

import (
	"errors"
	"fmt"
)

type (
	IPVersion  uint8
	IPProtocol uint8
	Verdict    uint8
	Endpoint   bool
)

const (
	IPv4 = IPVersion(4)
	IPv6 = IPVersion(6)

	InBound  = true
	OutBound = false

	Local  = true
	Remote = false

	// convenience
	IGMP   = IPProtocol(2)
	RAW    = IPProtocol(255)
	TCP    = IPProtocol(6)
	UDP    = IPProtocol(17)
	ICMP   = IPProtocol(1)
	ICMPv6 = IPProtocol(58)
)

const (
	DROP Verdict = iota
	BLOCK
	ACCEPT
	STOLEN
	QUEUE
	REPEAT
	STOP
)

var (
	ErrFailedToLoadPayload = errors.New("could not load packet payload")
)

// Returns the byte size of the ip, IPv4 = 4 bytes, IPv6 = 16
func (v IPVersion) ByteSize() int {
	switch v {
	case IPv4:
		return 4
	case IPv6:
		return 16
	}
	return 0
}

func (v IPVersion) String() string {
	switch v {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	}
	return fmt.Sprintf("<unknown ip version, %d>", uint8(v))
}

func (p IPProtocol) String() string {
	switch p {
	case RAW:
		return "RAW"
	case TCP:
		return "TCP"
	case UDP:
		return "UDP"
	case ICMP:
		return "ICMP"
	case ICMPv6:
		return "ICMPv6"
	case IGMP:
		return "IGMP"
	}
	return fmt.Sprintf("<unknown protocol, %d>", uint8(p))
}

func (v Verdict) String() string {
	switch v {
	case DROP:
		return "DROP"
	case ACCEPT:
		return "ACCEPT"
	}
	return fmt.Sprintf("<unsupported verdict, %d>", uint8(v))
}
