package netutils

import (
	"net"
	"testing"
)

func TestIPClassification(t *testing.T) {
	testClassification(t, net.IPv4(71, 87, 113, 211), global)
	testClassification(t, net.IPv4(127, 0, 0, 1), hostLocal)
	testClassification(t, net.IPv4(127, 255, 255, 1), hostLocal)
	testClassification(t, net.IPv4(192, 168, 172, 24), siteLocal)
}

func testClassification(t *testing.T, ip net.IP, expectedClassification int8) {
	c := classifyAddress(ip)
	if c != expectedClassification {
		t.Errorf("%s is %s, expected %s", ip, classificationString(c), classificationString(expectedClassification))
	}
}

func classificationString(c int8) string {
	switch c {
	case hostLocal:
		return "hostLocal"
	case linkLocal:
		return "linkLocal"
	case siteLocal:
		return "siteLocal"
	case global:
		return "global"
	case localMulticast:
		return "localMulticast"
	case globalMulticast:
		return "globalMulticast"
	case invalid:
		return "invalid"
	default:
		return "unknown"
	}
}
