// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package netutils

import "net"

// IP types
const (
	hostLocal int8 = iota
	linkLocal
	siteLocal
	global
	localMulticast
	globalMulticast
	invalid
)

func classifyAddress(ip net.IP) int8 {
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4
		switch {
		case ip4[0] == 127:
			// 127.0.0.0/8
			return hostLocal
		case ip4[0] == 169 && ip4[1] == 254:
			// 169.254.0.0/16
			return linkLocal
		case ip4[0] == 10:
			// 10.0.0.0/8
			return siteLocal
		case ip4[0] == 172 && ip4[1]&0xf0 == 16:
			// 172.16.0.0/12
			return siteLocal
		case ip4[0] == 192 && ip4[1] == 168:
			// 192.168.0.0/16
			return siteLocal
		case ip4[0] == 224:
			// 224.0.0.0/8
			return localMulticast
		case ip4[0] >= 225 && ip4[0] <= 239:
			// 225.0.0.0/8 - 239.0.0.0/8
			return globalMulticast
		case ip4[0] >= 240:
			// 240.0.0.0/8 - 255.0.0.0/8
			return invalid
		default:
			return global
		}
	} else if len(ip) == net.IPv6len {
		// IPv6
		switch {
		case ip.Equal(net.IPv6loopback):
			return hostLocal
		case ip[0]&0xfe == 0xfc:
			// fc00::/7
			return siteLocal
		case ip[0] == 0xfe && ip[1]&0xc0 == 0x80:
			// fe80::/10
			return linkLocal
		case ip[0] == 0xff && ip[1] <= 0x05:
			// ff00::/16 - ff05::/16
			return localMulticast
		case ip[0] == 0xff:
			// other ff00::/8
			return globalMulticast
		default:
			return global
		}
	}
	return invalid
}

// IPIsLocal returns true if the given IP is a site-local or link-local address
func IPIsLocal(ip net.IP) bool {
	switch classifyAddress(ip) {
	case siteLocal:
		return true
	case linkLocal:
		return true
	default:
		return false
	}
}

// IPIsGlobal returns true if the given IP is a global address
func IPIsGlobal(ip net.IP) bool {
	return classifyAddress(ip) == global
}

// IPIsLinkLocal returns true if the given IP is a link-local address
func IPIsLinkLocal(ip net.IP) bool {
	return classifyAddress(ip) == linkLocal
}

// IPIsSiteLocal returns true if the given IP is a site-local address
func IPIsSiteLocal(ip net.IP) bool {
	return classifyAddress(ip) == siteLocal
}
