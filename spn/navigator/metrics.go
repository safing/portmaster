package navigator

import (
	"sort"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/metrics"
)

var metricsRegistered = abool.New()

func registerMetrics() (err error) {
	// Only register metrics once.
	if !metricsRegistered.SetToIf(false, true) {
		return nil
	}

	// Map Stats.

	_, err = metrics.NewGauge(
		"spn/map/main/latency/all/lowest/seconds",
		nil,
		getLowestLatency,
		&metrics.Options{
			Name:       "SPN Map Lowest Latency",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/map/main/latency/fas/lowest/seconds",
		nil,
		getLowestLatencyFromFas,
		&metrics.Options{
			Name:       "SPN Map Lowest Latency",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/map/main/capacity/all/highest/bytes",
		nil,
		getHighestCapacity,
		&metrics.Options{
			Name:       "SPN Map Lowest Latency",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/map/main/capacity/fas/highest/bytes",
		nil,
		getHighestCapacityFromFas,
		&metrics.Options{
			Name:       "SPN Map Lowest Latency",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	return nil
}

var (
	mapStats        *mapMetrics
	mapStatsExpires time.Time
	mapStatsLock    sync.Mutex
	mapStatsTTL     = 55 * time.Second
)

type mapMetrics struct {
	lowestLatency            float64
	lowestForeignASLatency   float64
	highestCapacity          float64
	highestForeignASCapacity float64
}

func getLowestLatency() float64          { return getMapStats().lowestLatency }
func getLowestLatencyFromFas() float64   { return getMapStats().lowestForeignASLatency }
func getHighestCapacity() float64        { return getMapStats().highestCapacity }
func getHighestCapacityFromFas() float64 { return getMapStats().highestForeignASCapacity }

func getMapStats() *mapMetrics {
	mapStatsLock.Lock()
	defer mapStatsLock.Unlock()

	// Return cache if still valid.
	if time.Now().Before(mapStatsExpires) {
		return mapStats
	}

	// Refresh.
	mapStats = &mapMetrics{}

	// Get all pins and home.
	list := Main.pinList(true)
	home, _ := Main.GetHome()

	// Return empty stats if we have incomplete data.
	if len(list) <= 1 || home == nil {
		mapStatsExpires = time.Now().Add(mapStatsTTL)
		return mapStats
	}

	// Sort by latency.
	sort.Sort(sortByLowestMeasuredLatency(list))
	// Get lowest latency.
	lowestLatency, _ := list[0].measurements.GetLatency()
	mapStats.lowestLatency = lowestLatency.Seconds()
	// Find best foreign AS latency.
	bestForeignASPin := findFirstForeignASStatsPin(home, list)
	if bestForeignASPin != nil {
		lowestForeignASLatency, _ := bestForeignASPin.measurements.GetLatency()
		mapStats.lowestForeignASLatency = lowestForeignASLatency.Seconds()
	}

	// Sort by capacity.
	sort.Sort(sortByHighestMeasuredCapacity(list))
	// Get highest capacity.
	highestCapacity, _ := list[0].measurements.GetCapacity()
	mapStats.highestCapacity = float64(highestCapacity) / 8
	// Find best foreign AS capacity.
	bestForeignASPin = findFirstForeignASStatsPin(home, list)
	if bestForeignASPin != nil {
		highestForeignASCapacity, _ := bestForeignASPin.measurements.GetCapacity()
		mapStats.highestForeignASCapacity = float64(highestForeignASCapacity) / 8
	}

	mapStatsExpires = time.Now().Add(mapStatsTTL)
	return mapStats
}

func findFirstForeignASStatsPin(home *Pin, list []*Pin) *Pin {
	// Find best foreign AS latency.
	for _, pin := range list {
		compared := false

		// Skip if IPv4 AS matches.
		if home.LocationV4 != nil && pin.LocationV4 != nil {
			if home.LocationV4.AutonomousSystemNumber == pin.LocationV4.AutonomousSystemNumber {
				continue
			}
			compared = true
		}

		// Skip if IPv6 AS matches.
		if home.LocationV6 != nil && pin.LocationV6 != nil {
			if home.LocationV6.AutonomousSystemNumber == pin.LocationV6.AutonomousSystemNumber {
				continue
			}
			compared = true
		}

		// Skip if no data was compared
		if !compared {
			continue
		}

		return pin
	}
	return nil
}
