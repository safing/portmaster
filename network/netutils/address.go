package netutils

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

var errInvalidIP = errors.New("invalid IP address")

// IPFromAddr extracts or parses the IP address contained in the given address.
func IPFromAddr(addr net.Addr) (net.IP, error) {
	// Convert addr to IP if needed.
	switch v := addr.(type) {
	case *net.TCPAddr:
		return v.IP, nil
	case *net.UDPAddr:
		return v.IP, nil
	case *net.IPAddr:
		return v.IP, nil
	default:
		// Parse via string.
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return nil, fmt.Errorf("failed to split host and port of %q: %w", addr, err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, fmt.Errorf("address %q does not contain a valid IP address", addr)
		}
		return ip, nil
	}
}

// ParseHostPort parses a <ip>:port formatted address.
func ParseHostPort(address string) (net.IP, uint16, error) {
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
