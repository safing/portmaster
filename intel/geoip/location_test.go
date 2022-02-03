package geoip

import (
	"net"
	"testing"
)

func TestPrimitiveNetworkProximity(t *testing.T) {
	t.Parallel()

	ip4_1 := net.ParseIP("1.1.1.1")
	ip4_2 := net.ParseIP("1.1.1.2")
	ip4_3 := net.ParseIP("255.255.255.0")

	dist := PrimitiveNetworkProximity(ip4_1, ip4_2, 4)
	t.Logf("primitive proximity %s <> %s: %d", ip4_1, ip4_2, dist)
	if dist < 90 {
		t.Fatalf("unexpected distance between ip4_1 and ip4_2: %d", dist)
	}

	dist = PrimitiveNetworkProximity(ip4_1, ip4_3, 4)
	t.Logf("primitive proximity %s <> %s: %d", ip4_1, ip4_3, dist)
	if dist > 10 {
		t.Fatalf("unexpected distance between ip4_1 and ip4_3: %d", dist)
	}

	ip6_1 := net.ParseIP("2a02::1")
	ip6_2 := net.ParseIP("2a02::2")
	ip6_3 := net.ParseIP("ffff::1")

	dist = PrimitiveNetworkProximity(ip6_1, ip6_2, 6)
	t.Logf("primitive proximity %s <> %s: %d", ip6_1, ip6_2, dist)
	if dist < 90 {
		t.Fatalf("unexpected distance between ip6_1 and ip6_2: %d", dist)
	}

	dist = PrimitiveNetworkProximity(ip6_1, ip6_3, 6)
	t.Logf("primitive proximity %s <> %s: %d", ip6_1, ip6_3, dist)
	if dist > 20 {
		t.Fatalf("unexpected distance between ip6_1 and ip6_3: %d", dist)
	}
}
