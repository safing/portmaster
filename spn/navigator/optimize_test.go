package navigator

import (
	"strings"
	"sync"
	"testing"

	"github.com/safing/portmaster/spn/hub"
)

var (
	optimizedDefaultMapCreate sync.Once
	optimizedDefaultMap       *Map
)

func getOptimizedDefaultTestMap(t *testing.T) *Map {
	t.Helper()

	optimizedDefaultMapCreate.Do(func() {
		optimizedDefaultMap = createRandomTestMap(2, 100)
		optimizedDefaultMap.optimizeTestMap(t)
	})
	return optimizedDefaultMap
}

func (m *Map) optimizeTestMap(t *testing.T) {
	t.Helper()
	t.Logf("optimizing test map %s with %d pins", m.Name, len(m.all))

	// Save original Home, as we will be switching around the home for the
	// optimization.
	run := 0
	newLanes := 0
	originalHome := m.home
	mcf := newMeasurementCachedFactory()

	for {
		run++
		newLanesInRun := 0
		// Let's check if we have a run without any map changes.
		lastRun := true

		for _, pin := range m.all {
			// Set Home to this Pin for this iteration.
			if !m.SetHome(pin.Hub.ID, nil) {
				panic("failed to set home")
			}

			// Update measurements for the new home.
			updateMeasurements(m, mcf)

			optimizeResult, err := m.optimize(nil)
			if err != nil {
				panic(err)
			}
			lanesCreatedWithResult := 0
			for _, connectTo := range optimizeResult.SuggestedConnections {
				// Check if lane to suggested Hub already exists.
				if m.home.Hub.GetLaneTo(connectTo.Hub.ID) != nil {
					continue
				}

				// Add lanes to the Hub status.
				_ = m.home.Hub.AddLane(createLane(connectTo.Hub.ID))
				_ = connectTo.Hub.AddLane(createLane(m.home.Hub.ID))

				// Update Hubs in map.
				m.UpdateHub(m.home.Hub)
				m.UpdateHub(connectTo.Hub)
				newLanes++
				newLanesInRun++

				// We are changing the map in this run, so this is not the last.
				lastRun = false

				// Only create as many lanes as suggested by the result.
				lanesCreatedWithResult++
				if lanesCreatedWithResult >= optimizeResult.MaxConnect {
					break
				}
			}
			if optimizeResult.Purpose != OptimizePurposeTargetStructure {
				// If we aren't yet building the target structure, we need to keep building.
				lastRun = false
			}
		}

		// Log progress.
		if t != nil {
			t.Logf(
				"optimizing: added %d lanes in run #%d (%d Hubs) - %d new lanes in total",
				newLanesInRun,
				run,
				len(m.all),
				newLanes,
			)
		}

		// End optimization after last run.
		if lastRun {
			break
		}
	}

	// Log what was done and set home back to the original value.
	if t != nil {
		t.Logf("finished optimizing test map %s: added %d lanes in %d runs", m.Name, newLanes, run)
	}
	m.home = originalHome
}

func TestOptimize(t *testing.T) {
	t.Parallel()

	m := getOptimizedDefaultTestMap(t)
	matcher := m.defaultOptions().Destination.Matcher(m.intel)
	originalHome := m.home

	for _, pin := range m.all {
		// Set Home to this Pin for this iteration.
		m.home = pin
		err := m.recalculateReachableHubs()
		if err != nil {
			panic(err)
		}

		for _, peer := range m.all {
			// Check if the Pin matches the criteria.
			if !matcher(peer) {
				continue
			}

			// TODO: Adapt test to new regions.
			if peer.HopDistance > 5 {
				t.Errorf("Optimization error: %s is %d hops away from %s", peer, peer.HopDistance, pin)
			}
		}
	}

	// Print stats
	t.Logf("optimized map:\n%s\n", m.Stats())

	m.home = originalHome
}

func updateMeasurements(m *Map, mcf *measurementCachedFactory) {
	for _, pin := range m.all {
		pin.measurements = mcf.getOrCreate(m.home.Hub.ID, pin.Hub.ID)
	}
}

type measurementCachedFactory struct {
	cache map[string]*hub.Measurements
}

func newMeasurementCachedFactory() *measurementCachedFactory {
	return &measurementCachedFactory{
		cache: make(map[string]*hub.Measurements),
	}
}

func (mcf *measurementCachedFactory) getOrCreate(from, to string) *hub.Measurements {
	var id string
	comparison := strings.Compare(from, to)
	switch {
	case comparison == 0:
		return nil
	case comparison > 0:
		id = from + "-" + to
	case comparison < 0:
		id = to + "-" + from
	}

	m, ok := mcf.cache[id]
	if ok {
		return m
	}

	m = hub.NewMeasurements()
	m.Latency = createLatency()
	m.Capacity = createCapacity()
	m.CalculatedCost = CalculateLaneCost(
		m.Latency,
		m.Capacity,
	)
	mcf.cache[id] = m
	return m
}
