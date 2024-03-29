package netutils

import (
	"errors"
	"net"
	"strconv"

	"github.com/safing/portmaster/service/network/packet"
)

var errInvalidIP = errors.New("invalid IP address")

// IPPortFromAddr extracts or parses the IP address and port contained in the given address.
func IPPortFromAddr(addr net.Addr) (ip net.IP, port uint16, err error) {
	// Convert addr to IP if needed.
	switch v := addr.(type) {
	case *net.TCPAddr:
		return v.IP, uint16(v.Port), nil
	case *net.UDPAddr:
		return v.IP, uint16(v.Port), nil
	case *net.IPAddr:
		return v.IP, 0, nil
	case *net.UnixAddr:
		return nil, 0, errors.New("unix addresses don't have IPs")
	default:
		return ParseIPPort(addr.String())
	}
}

// ProtocolFromNetwork returns the protocol from the given net, as used in the "net" golang stdlib.
func ProtocolFromNetwork(net string) (protocol packet.IPProtocol) {
	switch net {
	case "tcp", "tcp4", "tcp6":
		return packet.TCP
	case "udp", "udp4", "udp6":
		return packet.UDP
	default:
		return 0
	}
}

// ParseIPPort parses a <ip>:port formatted address.
func ParseIPPort(address string) (net.IP, uint16, error) {
	ipString, portString, err := net.SplitHostPort(address)
	if err != nil {
		return nil, 0, err
	}

	ip := net.ParseIP(ipString)
	if ip == nil {
		return nil, 0, errInvalidIP
	}

	port, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		return nil, 0, err
	}

	return ip, uint16(port), nil
}
