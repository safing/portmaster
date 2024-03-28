package navigator

import (
	"fmt"
	"sort"
	"strings"
)

// MapStats holds generic map statistics.
type MapStats struct {
	Name            string
	States          map[PinState]int
	Lanes           map[int]int
	ActiveTerminals int
}

// Stats collects and returns statistics from the map.
func (m *Map) Stats() *MapStats {
	m.Lock()
	defer m.Unlock()

	// Create stats struct.
	stats := &MapStats{
		Name:   m.Name,
		States: make(map[PinState]int),
		Lanes:  make(map[int]int),
	}
	for _, state := range allStates {
		stats.States[state] = 0
	}

	// Iterate over all Pins to collect data.
	for _, pin := range m.all {
		// Count active terminals.
		if pin.HasActiveTerminal() {
			stats.ActiveTerminals++
		}

		// Check all states.
		for _, state := range allStates {
			if pin.State.Has(state) {
				stats.States[state]++
			}
		}

		// Count lanes.
		laneCnt, ok := stats.Lanes[len(pin.ConnectedTo)]
		if ok {
			stats.Lanes[len(pin.ConnectedTo)] = laneCnt + 1
		} else {
			stats.Lanes[len(pin.ConnectedTo)] = 1
		}
	}

	return stats
}

func (ms *MapStats) String() string {
	var builder strings.Builder

	// Write header.
	fmt.Fprintf(&builder, "Stats for Map %s:\n", ms.Name)

	// Write State Stats
	stateSummary := make([]string, 0, len(ms.States))
	for state, cnt := range ms.States {
		stateSummary = append(stateSummary, fmt.Sprintf("State %s: %d Hubs", state, cnt))
	}
	sort.Strings(stateSummary)
	for _, stateSum := range stateSummary {
		fmt.Fprintln(&builder, stateSum)
	}

	// Write Lane Stats
	laneStats := make([]string, 0, len(ms.Lanes))
	for laneCnt, pinCnt := range ms.Lanes {
		laneStats = append(laneStats, fmt.Sprintf("%d Lanes: %d Hubs", laneCnt, pinCnt))
	}
	sort.Strings(laneStats)
	for _, laneStat := range laneStats {
		fmt.Fprintln(&builder, laneStat)
	}

	return builder.String()
}
