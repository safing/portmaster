package navigator

type sortByPinID []*Pin

func (a sortByPinID) Len() int           { return len(a) }
func (a sortByPinID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a sortByPinID) Less(i, j int) bool { return a[i].Hub.ID < a[j].Hub.ID }

type sortByLowestMeasuredCost []*Pin

func (a sortByLowestMeasuredCost) Len() int      { return len(a) }
func (a sortByLowestMeasuredCost) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a sortByLowestMeasuredCost) Less(i, j int) bool {
	x := a[i].measurements.GetCalculatedCost()
	y := a[j].measurements.GetCalculatedCost()
	if x != y {
		return x < y
	}

	// Fall back to geo proximity.
	gx := a[i].measurements.GetGeoProximity()
	gy := a[j].measurements.GetGeoProximity()
	if gx != gy {
		return gx > gy
	}

	// Fall back to Hub ID.
	return a[i].Hub.ID < a[j].Hub.ID
}

type sortBySuggestedHopDistanceAndLowestMeasuredCost []*Pin

func (a sortBySuggestedHopDistanceAndLowestMeasuredCost) Len() int      { return len(a) }
func (a sortBySuggestedHopDistanceAndLowestMeasuredCost) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a sortBySuggestedHopDistanceAndLowestMeasuredCost) Less(i, j int) bool {
	// First sort by suggested hop distance.
	if a[i].analysis.SuggestedHopDistance != a[j].analysis.SuggestedHopDistance {
		return a[i].analysis.SuggestedHopDistance > a[j].analysis.SuggestedHopDistance
	}

	// Then by cost.
	x := a[i].measurements.GetCalculatedCost()
	y := a[j].measurements.GetCalculatedCost()
	if x != y {
		return x < y
	}

	// Fall back to geo proximity.
	gx := a[i].measurements.GetGeoProximity()
	gy := a[j].measurements.GetGeoProximity()
	if gx != gy {
		return gx > gy
	}

	// Fall back to Hub ID.
	return a[i].Hub.ID < a[j].Hub.ID
}

type sortBySuggestedHopDistanceInRegionAndLowestMeasuredCost []*Pin

func (a sortBySuggestedHopDistanceInRegionAndLowestMeasuredCost) Len() int { return len(a) }
func (a sortBySuggestedHopDistanceInRegionAndLowestMeasuredCost) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a sortBySuggestedHopDistanceInRegionAndLowestMeasuredCost) Less(i, j int) bool {
	// First sort by suggested hop distance.
	if a[i].analysis.SuggestedHopDistanceInRegion != a[j].analysis.SuggestedHopDistanceInRegion {
		return a[i].analysis.SuggestedHopDistanceInRegion > a[j].analysis.SuggestedHopDistanceInRegion
	}

	// Then by cost.
	x := a[i].measurements.GetCalculatedCost()
	y := a[j].measurements.GetCalculatedCost()
	if x != y {
		return x < y
	}

	// Fall back to geo proximity.
	gx := a[i].measurements.GetGeoProximity()
	gy := a[j].measurements.GetGeoProximity()
	if gx != gy {
		return gx > gy
	}

	// Fall back to Hub ID.
	return a[i].Hub.ID < a[j].Hub.ID
}

type sortByLowestMeasuredLatency []*Pin

func (a sortByLowestMeasuredLatency) Len() int      { return len(a) }
func (a sortByLowestMeasuredLatency) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a sortByLowestMeasuredLatency) Less(i, j int) bool {
	x, _ := a[i].measurements.GetLatency()
	y, _ := a[j].measurements.GetLatency()
	switch {
	case x == y:
		// Go to fallbacks.
	case x == 0:
		// Ignore zero values.
		return false // j/y is better.
	case y == 0:
		// Ignore zero values.
		return true // i/x is better.
	default:
		return x < y
	}

	// Fall back to geo proximity.
	gx := a[i].measurements.GetGeoProximity()
	gy := a[j].measurements.GetGeoProximity()
	if gx != gy {
		return gx > gy
	}

	// Fall back to Hub ID.
	return a[i].Hub.ID < a[j].Hub.ID
}

type sortByHighestMeasuredCapacity []*Pin

func (a sortByHighestMeasuredCapacity) Len() int      { return len(a) }
func (a sortByHighestMeasuredCapacity) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a sortByHighestMeasuredCapacity) Less(i, j int) bool {
	x, _ := a[i].measurements.GetCapacity()
	y, _ := a[j].measurements.GetCapacity()
	if x != y {
		return x > y
	}

	// Fall back to geo proximity.
	gx := a[i].measurements.GetGeoProximity()
	gy := a[j].measurements.GetGeoProximity()
	if gx != gy {
		return gx > gy
	}

	// Fall back to Hub ID.
	return a[i].Hub.ID < a[j].Hub.ID
}
