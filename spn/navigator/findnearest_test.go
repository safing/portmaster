package navigator

import (
	"testing"
)

func TestFindNearest(t *testing.T) {
	t.Parallel()

	// Create map and lock faking in order to guarantee reproducability of faked data.
	m := getDefaultTestMap()
	fakeLock.Lock()
	defer fakeLock.Unlock()

	for range 100 {
		// Create a random destination address
		ip4, loc4 := createGoodIP(true)

		nbPins, err := m.findNearestPins(loc4, nil, m.DefaultOptions(), DestinationHub, false)
		if err != nil {
			t.Error(err)
		} else {
			t.Logf("Pins near %s: %s", ip4, nbPins)
		}
	}

	for range 100 {
		// Create a random destination address
		ip6, loc6 := createGoodIP(true)

		nbPins, err := m.findNearestPins(nil, loc6, m.DefaultOptions(), DestinationHub, false)
		if err != nil {
			t.Error(err)
		} else {
			t.Logf("Pins near %s: %s", ip6, nbPins)
		}
	}
}

/*
TODO: Find a way to quickly generate good geoip data on the fly, as we don't want to measure IP address generation, but only finding the nearest pins.

func BenchmarkFindNearest(b *testing.B) {
	// Create map and lock faking in order to guarantee reproducability of faked data.
	m := getDefaultTestMap()
	fakeLock.Lock()
	defer fakeLock.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a random destination address
		var dstIP net.IP
		if i%2 == 0 {
			dstIP = net.ParseIP(gofakeit.IPv4Address())
		} else {
			dstIP = net.ParseIP(gofakeit.IPv6Address())
		}

		_, err := m.findNearestPins(dstIP, m.DefaultOptions(),DestinationHub		if err != nil {
			b.Error(err)
		}
	}
}
*/

func findFakeHomeHub(m *Map) {
	// Create fake IP address.
	_, loc4 := createGoodIP(true)
	_, loc6 := createGoodIP(false)

	nbPins, err := m.findNearestPins(loc4, loc6, m.defaultOptions(), HomeHub, false)
	if err != nil {
		panic(err)
	}
	if len(nbPins.pins) == 0 {
		panic("could not find a Home Hub")
	}

	// Set Home.
	m.home = nbPins.pins[0].pin

	// Recalculate reachability.
	if err := m.recalculateReachableHubs(); err != nil {
		panic(err)
	}
}

func TestNearbyPinsCleaning(t *testing.T) {
	t.Parallel()

	testCleaning(t, []float32{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 3)
	testCleaning(t, []float32{10, 11, 12, 13, 50, 60, 70, 80, 90, 100}, 4)
	testCleaning(t, []float32{10, 11, 12, 40, 50, 60, 70, 80, 90, 100}, 3)
	testCleaning(t, []float32{10, 11, 30, 40, 50, 60, 70, 80, 90, 100}, 3)
}

func testCleaning(t *testing.T, costs []float32, expectedLeftOver int) {
	t.Helper()

	nb := &nearbyPins{
		minPins:     3,
		maxPins:     5,
		cutOffLimit: 10,
	}

	// Simulate usage.
	for _, cost := range costs {
		// Add to list.
		nb.add(nil, cost)

		// Clean once in a while.
		if len(nb.pins) > nb.maxPins {
			nb.clean()
		}
	}
	// Final clean.
	nb.clean()

	// Check results.
	t.Logf("result: %+v", nb.pins)
	if len(nb.pins) != expectedLeftOver {
		t.Errorf("unexpected amount of left over pins: %+v", nb.pins)
	}
}
