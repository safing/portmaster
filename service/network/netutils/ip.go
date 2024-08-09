package netutils

import "net"

// IPScope is the scope of the IP address.
type IPScope int8

// Defined IP Scopes.
const (
	Invalid IPScope = iota - 1
	Undefined
	HostLocal
	LinkLocal
	SiteLocal
	Global
	LocalMulticast
	GlobalMulticast
)

// ClassifyIP returns the network scope of the given IP address.
// Deprecated: Please use the new GetIPScope instead.
func ClassifyIP(ip net.IP) IPScope {
	return GetIPScope(ip)
}

// GetIPScope returns the network scope of the given IP address.
func GetIPScope(ip net.IP) IPScope { //nolint:gocognit
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4
		switch {
		case ip4[0] == 0 && ip4[1] == 0 && ip4[2] == 0 && ip4[3] == 0:
			// 0.0.0.0/32
			return LocalMulticast // Used as source for L2 based protocols with no L3 addressing.
		case ip4[0] == 0:
			// 0.0.0.0/8
			return Invalid
		case ip4[0] == 10:
			// 10.0.0.0/8 (RFC1918)
			return SiteLocal
		case ip4[0] == 100 && ip4[1]&0b11000000 == 64:
			// 100.64.0.0/10 (RFC6598)
			return SiteLocal
		case ip4[0] == 127:
			// 127.0.0.0/8 (RFC1918)
			return HostLocal
		case ip4[0] == 169 && ip4[1] == 254:
			// 169.254.0.0/16 (RFC3927)
			return LinkLocal
		case ip4[0] == 172 && ip4[1]&0b11110000 == 16:
			// 172.16.0.0/12 (RFC1918)
			return SiteLocal
		case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2:
			// 192.0.2.0/24 (TEST-NET-1, RFC5737)
			return Invalid
		case ip4[0] == 192 && ip4[1] == 168:
			// 192.168.0.0/16 (RFC1918)
			return SiteLocal
		case ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100:
			// 198.51.100.0/24 (TEST-NET-2, RFC5737)
			return Invalid
		case ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113:
			// 203.0.113.0/24 (TEST-NET-3, RFC5737)
			return Invalid
		case ip4[0] == 224:
			// 224.0.0.0/8 (RFC5771)
			return LocalMulticast
		case ip4[0] == 233 && ip4[1] == 252 && ip4[2] == 0:
			// 233.252.0.0/24 (MCAST-TEST-NET; RFC5771, RFC6676)
			return Invalid
		case ip4[0] >= 225 && ip4[0] <= 238:
			// 225.0.0.0/8 - 238.0.0.0/8 (RFC5771)
			return GlobalMulticast
		case ip4[0] == 239:
			// 239.0.0.0/8 (RFC2365)
			return LocalMulticast
		case ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255:
			// 255.255.255.255/32
			return LocalMulticast
		case ip4[0] >= 240:
			// 240.0.0.0/8 - 255.0.0.0/8 (minus 255.255.255.255/32)
			return Invalid
		default:
			return Global
		}
	} else if len(ip) == net.IPv6len {
		// IPv6

		// TODO: Add IPv6 RFC5771 test / doc networks
		// 2001:db8::/32
		// 3fff::/20
		switch {
		case ip.Equal(net.IPv6zero):
			return Invalid
		case ip.Equal(net.IPv6loopback):
			return HostLocal
		case ip[0]&0xfe == 0xfc:
			// fc00::/7
			return SiteLocal
		case ip[0] == 0xfe && ip[1]&0xc0 == 0x80:
			// fe80::/10
			return LinkLocal
		case ip[0] == 0xff && ip[1] <= 0x05:
			// ff00::/16 - ff05::/16
			return LocalMulticast
		case ip[0] == 0xff:
			// other ff00::/8
			return GlobalMulticast
		default:
			return Global
		}
	}
	return Invalid
}

// IsLocalhost returns whether the IP refers to the host itself.
func (scope IPScope) IsLocalhost() bool {
	return scope == HostLocal
}

// IsLAN returns true if the scope is site-local or link-local.
func (scope IPScope) IsLAN() bool {
	switch scope { //nolint:exhaustive // Looking for something specific.
	case SiteLocal, LinkLocal, LocalMulticast:
		return true
	default:
		return false
	}
}

// IsGlobal returns true if the scope is global.
func (scope IPScope) IsGlobal() bool {
	switch scope { //nolint:exhaustive // Looking for something specific.
	case Global, GlobalMulticast:
		return true
	default:
		return false
	}
}

// GetBroadcastAddress returns the broadcast address of the given IP and network mask.
// If a mixed IPv4/IPv6 input is given, it returns nil.
func GetBroadcastAddress(ip net.IP, netMask net.IPMask) net.IP {
	// Convert to standard v4.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	mask := net.IP(netMask)
	if ip4Mask := mask.To4(); ip4Mask != nil {
		mask = ip4Mask
	}

	// Check for mixed v4/v6 input.
	if len(ip) != len(mask) {
		return nil
	}

	// Merge to broadcast address
	n := len(ip)
	broadcastAddress := make(net.IP, n)
	for i := range n {
		broadcastAddress[i] = ip[i] | ^mask[i]
	}
	return broadcastAddress
}
