package navigator

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit"

	"github.com/safing/jess/lhash"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/spn/hub"
)

var (
	fakeLock sync.Mutex

	defaultMapCreate sync.Once
	defaultMap       *Map
)

func getDefaultTestMap() *Map {
	defaultMapCreate.Do(func() {
		defaultMap = createRandomTestMap(1, 200)
	})
	return defaultMap
}

func TestRandomMapCreation(t *testing.T) {
	t.Parallel()

	m := getDefaultTestMap()

	fmt.Println("All Pins:")
	for _, pin := range m.all {
		fmt.Printf("%s: %s %s\n", pin, pin.Hub.Info.IPv4, pin.Hub.Info.IPv6)
	}

	// Print stats
	fmt.Printf("\n%s\n", m.Stats())

	// Print home
	fmt.Printf("Selected Home Hub: %s\n", m.home)
}

func createRandomTestMap(seed int64, size int) *Map {
	fakeLock.Lock()
	defer fakeLock.Unlock()

	// Seed with parameter to make it reproducible.
	gofakeit.Seed(seed)

	// Enforce minimum size.
	if size < 10 {
		size = 10
	}

	// Create Hub list.
	hubs := make([]*hub.Hub, 0, size)

	// Create Intel data structure.
	mapIntel := &hub.Intel{
		Hubs: make(map[string]*hub.HubIntel),
	}

	// Define periodic values.
	var currentGroup string

	// Create [size] fake Hubs.
	for i := range size {
		// Change group every 5 Hubs.
		if i%5 == 0 {
			currentGroup = gofakeit.Username()
		}

		// Create new fake Hub and add to the list.
		h := createFakeHub(currentGroup, true, mapIntel)
		hubs = append(hubs, h)
	}

	// Fake three superseeded Hubs.
	for i := range 3 {
		h := hubs[size-1-i]

		// Set FirstSeen in the past and copy an IP address of an existing Hub.
		h.FirstSeen = time.Now().Add(-1 * time.Hour)
		if i%2 == 0 {
			h.Info.IPv4 = hubs[i].Info.IPv4
		} else {
			h.Info.IPv6 = hubs[i].Info.IPv6
		}
	}

	// Create Lanes between Hubs in order to create the network.
	totalConnections := size * 10
	for range totalConnections {
		// Get new random indexes.
		indexA := gofakeit.Number(0, size-1)
		indexB := gofakeit.Number(0, size-1)
		if indexA == indexB {
			continue
		}

		// Get Hubs and check if they are already connected.
		hubA := hubs[indexA]
		hubB := hubs[indexB]
		if hubA.GetLaneTo(hubB.ID) != nil {
			// already connected
			continue
		}
		if hubB.GetLaneTo(hubA.ID) != nil {
			// already connected
			continue
		}

		// Create connections.
		_ = hubA.AddLane(createLane(hubB.ID))
		// Add the second connection in 99% of cases.
		// If this is missing, the Pins should not show up as connected.
		if gofakeit.Number(0, 100) != 0 {
			_ = hubB.AddLane(createLane(hubA.ID))
		}
	}

	// Parse constructed intel data
	err := mapIntel.ParseAdvisories()
	if err != nil {
		panic(err)
	}

	// Create map and add Pins.
	m := NewMap(fmt.Sprintf("Test-Map-%d", seed), true)
	m.intel = mapIntel
	for _, h := range hubs {
		m.UpdateHub(h)
	}

	// Fake communication error with three Hubs.
	var i int
	for _, pin := range m.all {
		pin.MarkAsFailingFor(1 * time.Hour)
		pin.addStates(StateFailing)

		if i++; i >= 3 {
			break
		}
	}

	// Set a Home Hub.
	findFakeHomeHub(m)

	return m
}

func createFakeHub(group string, randomFailes bool, mapIntel *hub.Intel) *hub.Hub {
	// Create fake Hub ID.
	idSrc := gofakeit.Password(true, true, true, true, true, 64)
	id := lhash.Digest(lhash.BLAKE2b_256, []byte(idSrc)).Base58()
	ip4, _ := createGoodIP(true)
	ip6, _ := createGoodIP(false)

	// Create and return new fake Hub.
	h := &hub.Hub{
		ID: id,
		Info: &hub.Announcement{
			ID:        id,
			Timestamp: time.Now().Unix(),
			Name:      gofakeit.Username(),
			Group:     group,
			// ContactAddress // TODO
			// ContactService // TODO
			// Hosters    []string // TODO
			// Datacenter string   // TODO
			IPv4: ip4,
			IPv6: ip6,
		},
		Status: &hub.Status{
			Timestamp: time.Now().Unix(),
			Keys: map[string]*hub.Key{
				"a": {
					Expires: time.Now().Add(48 * time.Hour).Unix(),
				},
			},
			Load: gofakeit.Number(10, 100),
		},
		Measurements: hub.NewMeasurements(),
		FirstSeen:    time.Now(),
	}
	h.Measurements.Latency = createLatency()
	h.Measurements.Capacity = createCapacity()
	h.Measurements.CalculatedCost = CalculateLaneCost(
		h.Measurements.Latency,
		h.Measurements.Capacity,
	)

	// Return if not failures of any kind should be simulated.
	if !randomFailes {
		return h
	}

	// Set hub-based states.
	if gofakeit.Number(0, 100) == 0 {
		// Fake Info message error.
		h.InvalidInfo = true
	}
	if gofakeit.Number(0, 100) == 0 {
		// Fake Status message error.
		h.InvalidStatus = true
	}
	if gofakeit.Number(0, 100) == 0 {
		// Fake expired exchange keys.
		for _, key := range h.Status.Keys {
			key.Expires = time.Now().Add(-1 * time.Hour).Unix()
		}
	}

	// Return if not failures of any kind should be simulated.
	if mapIntel == nil {
		return h
	}

	// Set advisory-based states.
	if gofakeit.Number(0, 10) == 0 {
		// Make Trusted State
		mapIntel.Hubs[h.ID] = &hub.HubIntel{
			Trusted: true,
		}
	}
	if gofakeit.Number(0, 100) == 0 {
		// Discourage any usage.
		mapIntel.HubAdvisory = append(mapIntel.HubAdvisory, "- "+h.Info.IPv4.String())
	}
	if gofakeit.Number(0, 100) == 0 {
		// Discourage Home Hub usage.
		mapIntel.HomeHubAdvisory = append(mapIntel.HomeHubAdvisory, "- "+h.Info.IPv4.String())
	}
	if gofakeit.Number(0, 100) == 0 {
		// Discourage Destination Hub usage.
		mapIntel.DestinationHubAdvisory = append(mapIntel.DestinationHubAdvisory, "- "+h.Info.IPv4.String())
	}

	return h
}

func createGoodIP(v4 bool) (net.IP, *geoip.Location) {
	var candidate net.IP
	for range 100 {
		if v4 {
			candidate = net.ParseIP(gofakeit.IPv4Address())
		} else {
			candidate = net.ParseIP(gofakeit.IPv6Address())
		}
		loc, err := geoip.GetLocation(candidate)
		if err == nil && loc.Coordinates.Latitude != 0 {
			return candidate, loc
		}
	}
	return candidate, nil
}

func createLane(toHubID string) *hub.Lane {
	return &hub.Lane{
		ID:       toHubID,
		Latency:  createLatency(),
		Capacity: createCapacity(),
	}
}

func createLatency() time.Duration {
	// Return a value between 10ms and 100ms.
	return time.Duration(gofakeit.Float64Range(10, 100) * float64(time.Millisecond))
}

func createCapacity() int {
	// Return a value between 10Mbit/s and 1Gbit/s.
	return gofakeit.Number(10000000, 1000000000)
}
