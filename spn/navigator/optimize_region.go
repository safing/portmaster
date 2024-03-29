package navigator

import (
	"fmt"
	"sort"
)

func (or *OptimizationResult) markSuggestedReachableInRegion(suggested *Pin, hopDistance int) {
	// Abort if suggested Pin has no region.
	if suggested.region == nil {
		return
	}

	// Don't update if distance is greater or equal than current one.
	if hopDistance >= suggested.analysis.SuggestedHopDistanceInRegion {
		return
	}

	// Set suggested hop distance.
	suggested.analysis.SuggestedHopDistanceInRegion = hopDistance

	// Increase distance and apply to matching Pins.
	hopDistance++
	for _, lane := range suggested.ConnectedTo {
		if lane.Pin.region != nil &&
			lane.Pin.region.ID == suggested.region.ID &&
			or.matcher(lane.Pin) {
			or.markSuggestedReachableInRegion(lane.Pin, hopDistance)
		}
	}
}

func (m *Map) optimizeForLowestCostInRegion(result *OptimizationResult) {
	if m.home == nil || m.home.region == nil {
		return
	}
	region := m.home.region

	// Add approach.
	result.addApproach(fmt.Sprintf("Connect to best (lowest cost) %d Hubs within the region.", region.internalMinLanesOnHub))

	// Sort by lowest cost.
	sort.Sort(sortByLowestMeasuredCost(region.regardedPins))

	// Add to suggested pins.
	if len(region.regardedPins) <= region.internalMinLanesOnHub {
		result.addSuggested("best in region", region.regardedPins...)
	} else {
		result.addSuggested("best in region", region.regardedPins[:region.internalMinLanesOnHub]...)
	}
}

func (m *Map) optimizeForDistanceConstraintInRegion(result *OptimizationResult, max int) {
	if m.home == nil || m.home.region == nil {
		return
	}
	region := m.home.region

	// Add approach.
	result.addApproach(fmt.Sprintf("Satisfy max hop constraint of %d within the region.", region.internalMaxHops))

	// Sort by lowest cost.
	sort.Sort(sortBySuggestedHopDistanceInRegionAndLowestMeasuredCost(region.regardedPins))

	for i := 0; i < max && i < len(region.regardedPins); i++ {
		// Return when all regarded Pins are within the distance constraint.
		if region.regardedPins[i].analysis.SuggestedHopDistanceInRegion <= region.internalMaxHops {
			return
		}

		// If not, suggest a connection to the best match.
		result.addSuggested("satisfy regional hop constraint", region.regardedPins[i])
	}
}

func (m *Map) optimizeForRegionConnectivity(result *OptimizationResult) {
	if m.home == nil || m.home.region == nil {
		return
	}
	region := m.home.region

	// Add approach.
	result.addApproach("Connect region to other regions.")

	// Optimize for every region.
checkRegions:
	for _, otherRegion := range m.regions {
		// Skip own region.
		if region.ID == otherRegion.ID {
			continue
		}

		// Collect data on connections to that region.
		lanesToRegion, highestCostWithinLaneLimit := m.countConnectionsToRegion(result, region, otherRegion)

		// Sort by lowest cost.
		sort.Sort(sortByLowestMeasuredCost(otherRegion.regardedPins))

		// Find cheapest connections with a free slot or better values.
		var lanesSuggested int
		for _, pin := range otherRegion.regardedPins {
			myCost := pin.measurements.GetCalculatedCost()

			// Check if we are done or region is satisfied.
			switch {
			case lanesSuggested >= region.regionalMaxLanesOnHub:
				// We hit our max.
				continue checkRegions
			case lanesToRegion >= otherRegion.regionalMinLanes && myCost >= highestCostWithinLaneLimit:
				// Region has enough lanes and we are not better.
				continue checkRegions
			}

			// Check if we can contribute on this Pin.
			switch {
			case pin.analysis.CrossRegionalConnections < otherRegion.regionalMaxLanesOnHub &&
				lanesToRegion < otherRegion.regionalMinLanes:
				// There is a free spot on this Pin and the region needs more connections.
				result.addSuggested("occupy cross-region lane on pin", pin)
				lanesSuggested++
				lanesToRegion++
				// Because our own Pin is not counted, this should be the default
				// suggestion for a stable network.

			case myCost < pin.analysis.CrossRegionalHighestCostInHubLimit:
				// We have a better connection to this Pin than at least one other existing connection (within the limit!).
				result.addSuggested("replace cross-region lane on pin", pin)
				lanesSuggested++
				lanesToRegion++

			case myCost < highestCostWithinLaneLimit &&
				pin.analysis.CrossRegionalConnections < otherRegion.regionalMaxLanesOnHub:
				// We have a better connection to this Pin than another existing region-to-region connection.
				result.addSuggested("replace unrelated cross-region lane", pin)
				lanesSuggested++
				lanesToRegion++
			}
		}
	}
}

// countConnectionsToRegion analyzes existing lanes from this to another
// region, with taking lanes from this Hub into account.
func (m *Map) countConnectionsToRegion(result *OptimizationResult, region *Region, otherRegion *Region) (lanesToRegion int, highestCostWithinLaneLimit float32) {
	for _, pin := range region.regardedPins {
		// Skip self.
		if m.home.Hub.ID == pin.Hub.ID {
			continue
		}

		// Find lanes to other region.
		for _, lane := range pin.ConnectedTo {
			if lane.Pin.region != nil &&
				lane.Pin.region.ID == otherRegion.ID &&
				result.matcher(lane.Pin) {
				// This is a lane from this region to a regarded Pin in the other region.
				lanesToRegion++

				// Count cross region connection.
				lane.Pin.analysis.CrossRegionalConnections++

				// Collect lane costs.
				lane.Pin.analysis.CrossRegionalLaneCosts = append(
					lane.Pin.analysis.CrossRegionalLaneCosts,
					lane.Cost,
				)
			}
		}
	}

	// Calculate lane costs from collected lane costs.
	for _, pin := range otherRegion.regardedPins {
		sort.Sort(sortCostsByLowest(pin.analysis.CrossRegionalLaneCosts))
		switch {
		case len(pin.analysis.CrossRegionalLaneCosts) == 0:
			// Nothing to do.
		case len(pin.analysis.CrossRegionalLaneCosts) < otherRegion.regionalMaxLanesOnHub:
			pin.analysis.CrossRegionalLowestCostLane = pin.analysis.CrossRegionalLaneCosts[0]
			pin.analysis.CrossRegionalHighestCostInHubLimit = pin.analysis.CrossRegionalLaneCosts[len(pin.analysis.CrossRegionalLaneCosts)-1]
		default:
			pin.analysis.CrossRegionalLowestCostLane = pin.analysis.CrossRegionalLaneCosts[0]
			pin.analysis.CrossRegionalHighestCostInHubLimit = pin.analysis.CrossRegionalLaneCosts[otherRegion.regionalMaxLanesOnHub-1]
		}

		// Find highest cost within limit.
		if pin.analysis.CrossRegionalHighestCostInHubLimit > highestCostWithinLaneLimit {
			highestCostWithinLaneLimit = pin.analysis.CrossRegionalHighestCostInHubLimit
		}
	}

	return lanesToRegion, highestCostWithinLaneLimit
}

func (m *Map) optimizeForSatelliteConnectivity(result *OptimizationResult) {
	if m.home == nil {
		return
	}
	// This is only for Hubs that are not in a region.
	if m.home.region != nil {
		return
	}

	// Add approach.
	result.addApproach("Connect satellite to regions.")

	// Optimize for every region.
	for _, region := range m.regions {
		// Sort by lowest cost.
		sort.Sort(sortByLowestMeasuredCost(region.regardedPins))

		// Add to suggested pins.
		if len(region.regardedPins) <= region.satelliteMinLanes {
			result.addSuggested("best to region "+region.ID, region.regardedPins...)
		} else {
			result.addSuggested("best to region "+region.ID, region.regardedPins[:region.satelliteMinLanes]...)
		}
	}
}

type sortCostsByLowest []float32

func (a sortCostsByLowest) Len() int           { return len(a) }
func (a sortCostsByLowest) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a sortCostsByLowest) Less(i, j int) bool { return a[i] < a[j] }
