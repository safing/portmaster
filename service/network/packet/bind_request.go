package packet

import (
	"net"
)

type BindRequest interface {
	ProcessID() uint64

	// ReplySplitTunnel sends a split tunnel response for the request.
	// If localInterfaceIP is nil (or zeroed 0.0.0.0), no redirection will be performed (i.e., the connection will go through as normal).
	// If localInterfaceIP is non-nil, the connection will be redirected to the specified local interface IP.
	ReplySplitTunnel(ipv4 *net.IP, ipv6 *net.IP) error
}
