package netutils

import (
	"fmt"
	"net"
)

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
			return nil, fmt.Errorf("failed to split host and port of %q: %s", addr, err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, fmt.Errorf("address %q does not contain a valid IP address", addr)
		}
		return ip, nil
	}
}
