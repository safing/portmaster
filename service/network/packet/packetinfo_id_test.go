package packet

import (
	"fmt"
	"math/rand"
	"net"
	"testing"
)

// referenceConnectionID is a copy of the original fmt.Sprintf implementation.
func referenceConnectionID(protocol IPProtocol, src net.IP, srcPort uint16, dst net.IP, dstPort uint16, inbound bool) string {
	if protocol == TCP || protocol == UDP {
		if inbound {
			return fmt.Sprintf("%d-%s-%d-%s-%d", protocol, dst, dstPort, src, srcPort)
		}
		return fmt.Sprintf("%d-%s-%d-%s-%d", protocol, src, srcPort, dst, dstPort)
	}

	if inbound {
		return fmt.Sprintf("%d-%s-%s", protocol, dst, src)
	}
	return fmt.Sprintf("%d-%s-%s", protocol, src, dst)
}

func testIPs() []net.IP {
	return []net.IP{
		nil,
		{},
		{1},
		{1, 2, 3},
		{1, 2, 3, 4, 5},
		make(net.IP, 15),
		make(net.IP, 17),
		make(net.IP, 20),
		net.IPv4(0, 0, 0, 0),
		net.IPv4(192, 168, 1, 255).To4(),
		net.IPv4(192, 168, 1, 255), // 16 byte v4-mapped
		net.IPv4(255, 255, 255, 255),
		net.IPv6zero,
		net.IPv6loopback,
		net.IPv6unspecified,
		net.ParseIP("::ffff:1.2.3.4"),
		net.ParseIP("::1.2.3.4"), // v4-compatible, not mapped
		net.ParseIP("64:ff9b::1.2.3.4"),
		net.ParseIP("2001:db8::1"),
		net.ParseIP("2001:db8:0:0:1:0:0:1"),
		net.ParseIP("2001:db8:0:1:1:1:1:1"),
		net.ParseIP("fe80::1"),
		net.ParseIP("ff02::fb"),
		net.ParseIP("2001:db8:aaaa:bbbb:cccc:dddd:eeee:ffff"),
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xfe, 1, 2, 3, 4}, // almost mapped
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 1, 2, 3, 4},
	}
}

func TestCreateConnectionIDEquivalence(t *testing.T) {
	t.Parallel()

	protocols := []IPProtocol{0, 1, ICMP, TCP, UDP, 41, 58, 132, 255}
	ports := []uint16{0, 1, 53, 443, 8080, 65535}

	check := func(proto IPProtocol, src net.IP, sp uint16, dst net.IP, dp uint16, inbound bool) {
		t.Helper()
		want := referenceConnectionID(proto, src, sp, dst, dp, inbound)
		if got := CreateConnectionID(proto, src, sp, dst, dp, inbound); got != want {
			t.Fatalf("mismatch proto=%d src=%v(%d) dst=%v(%d) in=%v: got %q want %q", proto, []byte(src), sp, []byte(dst), dp, inbound, got, want)
		}
	}

	ips := testIPs()
	for _, proto := range protocols {
		for _, src := range ips {
			for _, dst := range ips {
				for _, inbound := range []bool{false, true} {
					check(proto, src, 1234, dst, 80, inbound)
				}
			}
		}
		for _, sp := range ports {
			for _, dp := range ports {
				check(proto, ips[9], sp, ips[18], dp, false)
				check(proto, ips[9], sp, ips[18], dp, true)
			}
		}
	}

	// Randomized.
	rnd := rand.New(rand.NewSource(1)) //nolint:gosec
	randIP := func() net.IP {
		var ip net.IP
		switch rnd.Intn(6) {
		case 0:
			ip = make(net.IP, 4)
		case 1:
			ip = make(net.IP, 16)
		case 2: // v4-mapped
			ip = make(net.IP, 16)
			ip[10], ip[11] = 0xff, 0xff
			_, _ = rnd.Read(ip[12:]) // Never returns an error.
			return ip
		case 3: // sparse IPv6 to exercise :: compression
			ip = make(net.IP, 16)
			for i := range ip {
				if rnd.Intn(3) == 0 {
					ip[i] = byte(rnd.Intn(256)) //nolint:gosec // Intn(256) is always in byte range.
				}
			}
			return ip
		case 4:
			ip = make(net.IP, rnd.Intn(40))
		default:
			ip = make(net.IP, 16)
		}
		_, _ = rnd.Read(ip) // Never returns an error.
		return ip
	}
	for range 200000 {
		check(IPProtocol(rnd.Intn(256)), randIP(), uint16(rnd.Intn(65536)), randIP(), uint16(rnd.Intn(65536)), rnd.Intn(2) == 0) //nolint:gosec // Intn bounds are within the target type ranges.
	}
}

var benchSink string

func BenchmarkCreateConnectionID(b *testing.B) {
	v4s, v4d := net.IPv4(192, 168, 1, 23).To4(), net.IPv4(142, 250, 185, 78).To4()
	v6s, v6d := net.ParseIP("fd00:1234:5678::23"), net.ParseIP("2a00:1450:4001:82b::200e")

	b.Run("IPv4", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchSink = CreateConnectionID(TCP, v4s, 51234, v4d, 443, false)
		}
	})
	b.Run("IPv6", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchSink = CreateConnectionID(TCP, v6s, 51234, v6d, 443, false)
		}
	})
}
