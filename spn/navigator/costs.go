package navigator

import "time"

const (
	nearestPinsMaxCostDifference = 5000
	nearestPinsMinimum           = 10
)

// CalculateLaneCost calculates the cost of using a Lane based on the given
// Lane latency and capacity.
// Ranges from 0 to 10000.
func CalculateLaneCost(latency time.Duration, capacity int) (cost float32) {
	// - One point for every ms in latency (linear)
	if latency != 0 {
		cost += float32(latency) / float32(time.Millisecond)
	} else {
		// Add cautious default cost if latency is not available.
		cost += 1000
	}

	capacityFloat := float32(capacity)
	switch {
	case capacityFloat == 0:
		// Add cautious default cost if capacity is not available.
		cost += 4000
	case capacityFloat < cap1Mbit:
		// - Between 1000 and 10000 points for ranges below 1Mbit/s
		cost += 1000 + 9000*((cap1Mbit-capacityFloat)/cap1Mbit)
	case capacityFloat < cap10Mbit:
		// - Between 100 and 1000 points for ranges below 10Mbit/s
		cost += 100 + 900*((cap10Mbit-capacityFloat)/cap10Mbit)
	case capacityFloat < cap100Mbit:
		// - Between 20 and 100 points for ranges below 100Mbit/s
		cost += 20 + 80*((cap100Mbit-capacityFloat)/cap100Mbit)
	case capacityFloat < cap1Gbit:
		// - Between 5 and 20 points for ranges below 1Gbit/s
		cost += 5 + 15*((cap1Gbit-capacityFloat)/cap1Gbit)
	case capacityFloat < cap10Gbit:
		// - Between 0 and 5 points for ranges below 10Gbit/s
		cost += 5 * ((cap10Gbit - float32(capacity)) / cap10Gbit)
	}

	return cost
}

// CalculateHubCost calculates the cost of using a Hub based on the given Hub load.
// Ranges from 100 to 10000.
func CalculateHubCost(load int) (cost float32) {
	switch {
	case load >= 100:
		return 10000
	case load >= 95:
		return 1000
	case load >= 80:
		return 500
	default:
		return 100
	}
}

// CalculateDestinationCost calculates the cost of a destination hub to a
// destination server based on the given proximity.
// Ranges from 0 to 2500.
func CalculateDestinationCost(proximity float32) (cost float32) {
	// Invert from proximity (0-100) to get a distance value.
	distance := 100 - proximity

	// Take the distance to the power of three and then divide by hundred in order to
	// make high distances exponentially more expensive.
	return (distance * distance * distance) / 100
}
