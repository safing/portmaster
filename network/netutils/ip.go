// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package netutils

import "net"

// IP classifications
const (
	HostLocal int8 = iota
	LinkLocal
	SiteLocal
	Global
	LocalMulticast
	GlobalMulticast
	Invalid
)

// ClassifyIP returns the classification for the given IP address.
func ClassifyIP(ip net.IP) int8 {
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
		case ip4[0] >= 225 && ip4[0] <= 239:
			// 225.0.0.0/8 - 239.0.0.0/8
			return GlobalMulticast
		case ip4[0] >= 240:
			// 240.0.0.0/8 - 255.0.0.0/8
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

// IPIsLocalhost returns whether the IP refers to the host itself.
func IPIsLocalhost(ip net.IP) bool {
	return ClassifyIP(ip) == HostLocal
}

// IPIsLAN returns true if the given IP is a site-local or link-local address.
func IPIsLAN(ip net.IP) bool {
	switch ClassifyIP(ip) {
	case SiteLocal:
		return true
	case LinkLocal:
		return true
	default:
		return false
	}
}

// IPIsGlobal returns true if the given IP is a global address.
func IPIsGlobal(ip net.IP) bool {
	return ClassifyIP(ip) == Global
}

// IPIsLinkLocal returns true if the given IP is a link-local address.
func IPIsLinkLocal(ip net.IP) bool {
	return ClassifyIP(ip) == LinkLocal
}

// IPIsSiteLocal returns true if the given IP is a site-local address.
func IPIsSiteLocal(ip net.IP) bool {
	return ClassifyIP(ip) == SiteLocal
}
