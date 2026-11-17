package netenv

import "net"

var (
	localAddrFactory  func(network string) net.Addr
	localPortReleaser func(network string, port uint16, unusable bool)
)

// SetLocalAddrFactory supplies the environment package with a function to get permitted local addresses for connections.
func SetLocalAddrFactory(laf func(network string) net.Addr) {
	if localAddrFactory == nil {
		localAddrFactory = laf
	}
}

// SetLocalPortReleaser supplies the environment package with a function to
// release a permitted local address that was not used, because the
// connection could not be established.
func SetLocalPortReleaser(release func(network string, port uint16, unusable bool)) {
	if localPortReleaser == nil {
		localPortReleaser = release
	}
}

func getLocalAddr(network string) net.Addr {
	if localAddrFactory != nil {
		return localAddrFactory(network)
	}
	return nil
}

// releaseLocalAddr releases a permitted local address returned by
// getLocalAddr. If unusable is set, the port could not be bound.
func releaseLocalAddr(addr net.Addr, unusable bool) {
	if localPortReleaser == nil {
		return
	}

	switch a := addr.(type) {
	case *net.TCPAddr:
		if a != nil {
			localPortReleaser("tcp", uint16(a.Port), unusable) //nolint:gosec // Ports of net.Addr are within the uint16 range.
		}
	case *net.UDPAddr:
		if a != nil {
			localPortReleaser("udp", uint16(a.Port), unusable) //nolint:gosec // Ports of net.Addr are within the uint16 range.
		}
	}
}
