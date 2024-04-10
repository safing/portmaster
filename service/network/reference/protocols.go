package reference

import (
	"strconv"
	"strings"
)

var (
	protocolNames = map[uint8]string{
		1:   "ICMP",
		2:   "IGMP",
		6:   "TCP",
		17:  "UDP",
		27:  "RDP",
		58:  "ICMP6",
		33:  "DCCP",
		136: "UDP-LITE",
	}

	protocolNumbers = map[string]uint8{
		"ICMP":     1,
		"IGMP":     2,
		"TCP":      6,
		"UDP":      17,
		"RDP":      27,
		"DCCP":     33,
		"ICMP6":    58,
		"UDP-LITE": 136,
	}
)

// GetProtocolName returns the name of a IP protocol number.
func GetProtocolName(protocol uint8) (name string) {
	name, ok := protocolNames[protocol]
	if ok {
		return name
	}
	return strconv.Itoa(int(protocol))
}

// GetProtocolNumber returns the number of a IP protocol name.
func GetProtocolNumber(protocol string) (number uint8, ok bool) {
	number, ok = protocolNumbers[strings.ToUpper(protocol)]
	if ok {
		return number, true
	}
	return 0, false
}

// IsPacketProtocol returns whether the given protocol number is a known packet based protocol.
// Note: Not fully complete. Calling IsPacketProtocol() does not equal calling !IsStreamProtocol().
func IsPacketProtocol(protocol uint8) bool {
	switch protocol {
	case 1, // ICMP
		17,  // UDP
		27,  // RDP
		58,  // ICMP6
		33,  // DCCP
		136: // UDP-LITE
		return true
	default:
		return false
	}
}

// IsStreamProtocol returns whether the given protocol number is a known stream based protocol.
// Note: Not fully complete. Calling IsPacketProtocol() does not equal calling !IsStreamProtocol().
func IsStreamProtocol(protocol uint8) bool {
	switch protocol {
	case 6: // TCP
		return true
	default:
		return false
	}
}

// IsICMP returns whether the given protocol is ICMP or ICMPv6.
func IsICMP(protocol uint8) bool {
	switch protocol {
	case 1, // ICMP
		58: // ICMP6
		return true
	default:
		return false
	}
}
