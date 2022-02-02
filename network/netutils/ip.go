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
		case ip4[0] == 127:
			// 127.0.0.0/8
			return HostLocal
		case ip4[0] == 169 && ip4[1] == 254:
			// 169.254.0.0/16
			return LinkLocal
		case ip4[0] == 10:
			// 10.0.0.0/8
			return SiteLocal
		case ip4[0] == 172 && ip4[1]&0xf0 == 16:
			// 172.16.0.0/12
			return SiteLocal
		case ip4[0] == 192 && ip4[1] == 168:
			// 192.168.0.0/16
			return SiteLocal
		case ip4[0] == 224:
			// 224.0.0.0/8
			return LocalMulticast
		case ip4[0] >= 225 && ip4[0] <= 238:
			// 225.0.0.0/8 - 238.0.0.0/8
			return GlobalMulticast
		case ip4[0] == 239:
			// 239.0.0.0/8
			// RFC2365 - https://tools.ietf.org/html/rfc2365
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
		switch {
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
