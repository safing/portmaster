package navigator

import (
	"net"
	"testing"
)

func TestFindRoutes(t *testing.T) {
	t.Parallel()

	// Create map and lock faking in order to guarantee reproducability of faked data.
	m := getOptimizedDefaultTestMap(t)
	fakeLock.Lock()
	defer fakeLock.Unlock()

	for i := 0; i < 1; i++ {
		// Create a random destination address
		dstIP, _ := createGoodIP(i%2 == 0)

		routes, err := m.FindRoutes(dstIP, m.DefaultOptions())
		switch {
		case err != nil:
			t.Error(err)
		case len(routes.All) == 0:
			t.Logf("No routes for %s", dstIP)
		default:
			t.Logf("Best route for %s: %s", dstIP, routes.All[0])
		}
	}
}

func BenchmarkFindRoutes(b *testing.B) {
	// Create map and lock faking in order to guarantee reproducability of faked data.
	m := getOptimizedDefaultTestMap(nil)
	fakeLock.Lock()
	defer fakeLock.Unlock()

	// Pre-generate 100 IPs
	preGenIPs := make([]net.IP, 0, 100)
	for i := 0; i < cap(preGenIPs); i++ {
		ip, _ := createGoodIP(i%2 == 0)
		preGenIPs = append(preGenIPs, ip)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		routes, err := m.FindRoutes(preGenIPs[i%len(preGenIPs)], m.DefaultOptions())
		if err != nil {
			b.Error(err)
		} else {
			b.Logf("Best route for %s: %s", preGenIPs[i%len(preGenIPs)], routes.All[0])
		}
	}
}
