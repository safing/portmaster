package netutils

import (
	"net"
	"testing"
)

func TestIPScope(t *testing.T) {
	t.Parallel()

	testScope(t, net.IPv4(71, 87, 113, 211), Global)
	testScope(t, net.IPv4(127, 0, 0, 1), HostLocal)
	testScope(t, net.IPv4(127, 255, 255, 1), HostLocal)
	testScope(t, net.IPv4(192, 168, 172, 24), SiteLocal)
	testScope(t, net.IPv4(172, 15, 1, 1), Global)
	testScope(t, net.IPv4(172, 16, 1, 1), SiteLocal)
	testScope(t, net.IPv4(172, 31, 1, 1), SiteLocal)
	testScope(t, net.IPv4(172, 32, 1, 1), Global)
}

func testScope(t *testing.T, ip net.IP, expectedScope IPScope) {
	t.Helper()

	c := GetIPScope(ip)
	if c != expectedScope {
		t.Errorf("%s is %s, expected %s", ip, scopeName(c), scopeName(expectedScope))
	}
}

func scopeName(c IPScope) string {
	switch c {
	case Invalid:
		return "invalid"
	case Undefined:
		return "undefined"
	case HostLocal:
		return "hostLocal"
	case LinkLocal:
		return "linkLocal"
	case SiteLocal:
		return "siteLocal"
	case Global:
		return "global"
	case LocalMulticast:
		return "localMulticast"
	case GlobalMulticast:
		return "globalMulticast"
	default:
		return "undefined"
	}
}
