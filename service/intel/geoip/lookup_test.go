package geoip

import (
	"net"
	"testing"
	"time"
)

func TestLocationLookup(t *testing.T) {
	// Skip in CI.
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	// Wait for db to be initialized
	worker.v4.rw.Lock()
	waiter := worker.v4.getWaiter()
	worker.v4.rw.Unlock()

	worker.triggerUpdate()
	select {
	case <-waiter:
	case <-time.After(15 * time.Second):
	}

	ip1 := net.ParseIP("81.2.69.142")
	loc1, err := GetLocation(ip1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", loc1)

	ip2 := net.ParseIP("1.1.1.1")
	loc2, err := GetLocation(ip2)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", loc2)

	ip3 := net.ParseIP("8.8.8.8")
	loc3, err := GetLocation(ip3)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", loc3)

	ip4 := net.ParseIP("81.2.70.142")
	loc4, err := GetLocation(ip4)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", loc4)

	ip5 := net.ParseIP("194.232.1.1")
	loc5, err := GetLocation(ip5)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", loc5)

	ip6 := net.ParseIP("151.101.1.164")
	loc6, err := GetLocation(ip6)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", loc6)

	dist1 := loc1.EstimateNetworkProximity(loc2)
	dist2 := loc2.EstimateNetworkProximity(loc3)
	dist3 := loc1.EstimateNetworkProximity(loc3)
	dist4 := loc1.EstimateNetworkProximity(loc4)

	t.Logf("proximity %s <> %s: %.2f", ip1, ip2, dist1)
	t.Logf("proximity %s <> %s: %.2f", ip2, ip3, dist2)
	t.Logf("proximity %s <> %s: %.2f", ip1, ip3, dist3)
	t.Logf("proximity %s <> %s: %.2f", ip1, ip4, dist4)
}
