package conf

import (
	"net"
	"sync"

	"github.com/tevino/abool"
)

var (
	hubHasV4 = abool.New()
	hubHasV6 = abool.New()
)

// SetHubNetworks sets the available IP networks on the Hub.
func SetHubNetworks(v4, v6 bool) {
	hubHasV4.SetTo(v4)
	hubHasV6.SetTo(v6)
}

// HubHasIPv4 returns whether the Hub has IPv4 support.
func HubHasIPv4() bool {
	return hubHasV4.IsSet()
}

// HubHasIPv6 returns whether the Hub has IPv6 support.
func HubHasIPv6() bool {
	return hubHasV6.IsSet()
}

var (
	bindIPv4   net.IP
	bindIPv6   net.IP
	bindIPLock sync.Mutex
)

// SetBindAddr sets the preferred connect (bind) addresses.
func SetBindAddr(ip4, ip6 net.IP) {
	bindIPLock.Lock()
	defer bindIPLock.Unlock()

	bindIPv4 = ip4
	bindIPv6 = ip6
}

// BindAddrIsSet returns whether any bind address is set.
func BindAddrIsSet() bool {
	bindIPLock.Lock()
	defer bindIPLock.Unlock()

	return bindIPv4 != nil || bindIPv6 != nil
}

// GetBindAddr returns an address with the preferred binding address for the
// given dial network.
// The dial network must have a suffix specifying the IP version.
func GetBindAddr(dialNetwork string) net.Addr {
	bindIPLock.Lock()
	defer bindIPLock.Unlock()

	switch dialNetwork {
	case "ip4":
		if bindIPv4 != nil {
			return &net.IPAddr{IP: bindIPv4}
		}
	case "ip6":
		if bindIPv6 != nil {
			return &net.IPAddr{IP: bindIPv6}
		}
	case "tcp4":
		if bindIPv4 != nil {
			return &net.TCPAddr{IP: bindIPv4}
		}
	case "tcp6":
		if bindIPv6 != nil {
			return &net.TCPAddr{IP: bindIPv6}
		}
	case "udp4":
		if bindIPv4 != nil {
			return &net.UDPAddr{IP: bindIPv4}
		}
	case "udp6":
		if bindIPv6 != nil {
			return &net.UDPAddr{IP: bindIPv6}
		}
	}

	return nil
}

// GetBindIPs returns the preferred binding IPs.
// Returns a slice with a single nil IP if no preferred binding IPs are set.
func GetBindIPs() []net.IP {
	bindIPLock.Lock()
	defer bindIPLock.Unlock()

	switch {
	case bindIPv4 == nil && bindIPv6 == nil:
		// Match most common case first.
		return []net.IP{nil}
	case bindIPv4 != nil && bindIPv6 != nil:
		return []net.IP{bindIPv4, bindIPv6}
	case bindIPv4 != nil:
		return []net.IP{bindIPv4}
	case bindIPv6 != nil:
		return []net.IP{bindIPv6}
	}

	return []net.IP{nil}
}
