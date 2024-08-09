package navigator

import (
	"fmt"
	"sort"
	"time"

	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
)

const (
	optimizationLowestCostConnections = 3
	optimizationHopDistanceTarget     = 3
	waitUntilMeasuredUpToPercent      = 0.5

	desegrationAttemptBackoff = time.Hour
)

// Optimization Purposes.
const (
	OptimizePurposeBootstrap       = "bootstrap"
	OptimizePurposeDesegregate     = "desegregate"
	OptimizePurposeWait            = "wait"
	OptimizePurposeTargetStructure = "target-structure"
)

// AnalysisState holds state for analyzing the network for optimizations.
type AnalysisState struct { //nolint:maligned
	// Suggested signifies that a direct connection to this Hub is suggested by
	// the optimization algorithm.
	Suggested bool

	// SuggestedHopDistance holds the hop distance to this Hub when only
	// considering the suggested Hubs as connected.
	SuggestedHopDistance int

	// SuggestedHopDistanceInRegion holds the hop distance to this Hub in the
	// same region when only considering the suggested Hubs as connected.
	SuggestedHopDistanceInRegion int

	// CrossRegionalConnections holds the amount of connections a Pin has from
	// the current region.
	CrossRegionalConnections int
	// CrossRegionalLowestCostLane holds the lowest cost of the counted
	// connections from the current region.
	CrossRegionalLowestCostLane float32
	// CrossRegionalLaneCosts holds all the cross regional lane costs.
	CrossRegionalLaneCosts []float32
	// CrossRegionalHighestCostInHubLimit holds to highest cost of the lowest
	// cost connections within the maximum allowed lanes on a Hub from the
	// current region.
	CrossRegionalHighestCostInHubLimit float32
}

// initAnalysis creates all Pin.analysis fields.
// The caller needs to hold the map and analysis lock..
func (m *Map) initAnalysis(result *OptimizationResult) {
	// Compile lists of regarded pins.
	m.regardedPins = make([]*Pin, 0, len(m.all))
	for _, region := range m.regions {
		region.regardedPins = make([]*Pin, 0, len(m.all))
	}
	// Find all regarded pins.
	for _, pin := range m.all {
		if result.matcher(pin) {
			m.regardedPins = append(m.regardedPins, pin)
			// Add to region.
			if pin.region != nil {
				pin.region.regardedPins = append(pin.region.regardedPins, pin)
			}
		}
	}

	// Initialize analysis state.
	for _, pin := range m.all {
		pin.analysis = &AnalysisState{}
	}
}

// clearAnalysis reset all Pin.analysis fields.
// The caller needs to hold the map and analysis lock.
func (m *Map) clearAnalysis() {
	m.regardedPins = nil
	for _, region := range m.regions {
		region.regardedPins = nil
	}
	for _, pin := range m.all {
		pin.analysis = nil
	}
}

// OptimizationResult holds the result of an optimizaion analysis.
type OptimizationResult struct {
	// Purpose holds a semi-human readable constant of the optimization purpose.
	Purpose string

	// Approach holds human readable descriptions of how the stated purpose
	// should be achieved.
	Approach []string

	// SuggestedConnections holds the Hubs to which connections are suggested.
	SuggestedConnections []*SuggestedConnection

	// MaxConnect specifies how many connections should be created at maximum
	// based on this optimization.
	MaxConnect int

	// StopOthers specifies if other connections than the suggested ones may
	// be stopped.
	StopOthers bool

	// opts holds the options for matching Hubs in this optimization.
	opts *HubOptions

	// matcher is the matcher used to create the regarded Pins.
	// Required for updating suggested hop distance.
	matcher PinMatcher
}

// SuggestedConnection holds suggestions by the optimization system.
type SuggestedConnection struct {
	// Hub holds the Hub to which a connection is suggested.
	Hub *hub.Hub
	// pin holds the Pin of the Hub.
	pin *Pin
	// Reason holds a reason why this connection is suggested.
	Reason string
	// Duplicate marks duplicate entries. These should be ignored when
	// connecting, but are helpful for understand the optimization result.
	Duplicate bool
}

func (or *OptimizationResult) addApproach(description string) {
	or.Approach = append(or.Approach, description)
}

func (or *OptimizationResult) addSuggested(reason string, pins ...*Pin) {
	for _, pin := range pins {
		// Mark as suggested.
		pin.analysis.Suggested = true

		// Check if this is a duplicate.
		var duplicate bool
		for _, sc := range or.SuggestedConnections {
			if pin.Hub.ID == sc.Hub.ID {
				duplicate = true
				break
			}
		}

		// Add to suggested connections.
		or.SuggestedConnections = append(or.SuggestedConnections, &SuggestedConnection{
			Hub:       pin.Hub,
			pin:       pin,
			Reason:    reason,
			Duplicate: duplicate,
		})

		// Update hop distances if we have a matcher.
		if or.matcher != nil {
			or.markSuggestedReachable(pin, 2)
			or.markSuggestedReachableInRegion(pin, 2)
		}
	}
}

func (or *OptimizationResult) markSuggestedReachable(suggested *Pin, hopDistance int) {
	// Don't update if distance is greater or equal than current one.
	if hopDistance >= suggested.analysis.SuggestedHopDistance {
		return
	}

	// Set suggested hop distance.
	suggested.analysis.SuggestedHopDistance = hopDistance

	// Increase distance and apply to matching Pins.
	hopDistance++
	for _, lane := range suggested.ConnectedTo {
		if or.matcher(lane.Pin) {
			or.markSuggestedReachable(lane.Pin, hopDistance)
		}
	}
}

// Optimize analyzes the map and suggests changes.
func (m *Map) Optimize(opts *HubOptions) (result *OptimizationResult, err error) {
	m.RLock()
	defer m.RUnlock()

	// Check if the map is empty.
	if m.isEmpty() {
		return nil, ErrEmptyMap
	}

	// Set default options if unset.
	if opts == nil {
		opts = &HubOptions{}
	}

	return m.optimize(opts)
}

func (m *Map) optimize(opts *HubOptions) (result *OptimizationResult, err error) {
	if m.home == nil {
		return nil, ErrHomeHubUnset
	}

	// Set default options if unset.
	if opts == nil {
		opts = &HubOptions{}
	}

	// Create result.
	result = &OptimizationResult{
		opts:    opts,
		matcher: opts.Matcher(TransitHub, m.intel),
	}

	// Setup analyis.
	m.analysisLock.Lock()
	defer m.analysisLock.Unlock()
	m.initAnalysis(result)
	defer m.clearAnalysis()

	// Bootstrap to the network and desegregate map.
	// If there is a result, return it immediately.
	returnImmediately := m.optimizeForBootstrappingAndDesegregation(result)
	if returnImmediately {
		return result, nil
	}

	// Check if we have the measurements we need.
	if m.measuringEnabled {
		// Cound pins with valid measurements.
		var validMeasurements float32
		for _, pin := range m.regardedPins {
			if pin.measurements.Valid() {
				validMeasurements++
			}
		}

		// If less than the required amount of regarded Pins have valid
		// measurements, let's wait until we have that.
		if validMeasurements/float32(len(m.regardedPins)) < waitUntilMeasuredUpToPercent {
			return &OptimizationResult{
				Purpose:  OptimizePurposeWait,
				Approach: []string{"Wait for measurements of 80% of regarded nodes for better optimization."},
			}, nil
		}
	}

	// Set default values for target structure optimization.
	result.Purpose = OptimizePurposeTargetStructure
	result.MaxConnect = 3
	result.StopOthers = true

	// Optimize for lowest cost.
	m.optimizeForLowestCost(result, optimizationLowestCostConnections)

	// Optimize for lowest cost in region.
	m.optimizeForLowestCostInRegion(result)

	// Optimize for distance constraint in region.
	m.optimizeForDistanceConstraintInRegion(result, 3)

	// Optimize for region-to-region connectivity.
	m.optimizeForRegionConnectivity(result)

	// Optimize for satellite-to-region connectivity.
	m.optimizeForSatelliteConnectivity(result)

	// Lapse traffic stats after optimizing for good fresh data next time.
	for _, crane := range docks.GetAllAssignedCranes() {
		crane.NetState.LapsePeriod()
	}

	// Clean and return.
	return result, nil
}

func (m *Map) optimizeForBootstrappingAndDesegregation(result *OptimizationResult) (returnImmediately bool) {
	// All regarded Pins are reachable.
	reachable := len(m.regardedPins)

	// Count Pins that may be connectable.
	connectable := make([]*Pin, 0, len(m.all))
	// Copy opts as we are going to make changes.
	opts := result.opts.Copy()
	opts.NoDefaults = true
	opts.Regard = StateNone
	opts.Disregard = StateSummaryDisregard
	// Collect Pins with matcher.
	matcher := opts.Matcher(TransitHub, m.intel)
	for _, pin := range m.all {
		if matcher(pin) {
			connectable = append(connectable, pin)
		}
	}

	switch {
	case reachable == 0:

		// Sort by lowest cost.
		sort.Sort(sortByLowestMeasuredCost(connectable))

		// Return bootstrap optimization.
		result.Purpose = OptimizePurposeBootstrap
		result.Approach = []string{"Connect to a near Hub to connect to the network."}
		result.MaxConnect = 1
		result.addSuggested("bootstrap", connectable...)
		return true

	case reachable > len(connectable)/2:
		// We are part of the majority network, continue with regular optimization.

	case time.Now().Add(-desegrationAttemptBackoff).Before(m.lastDesegrationAttempt):
		// We tried to desegregate recently, continue with regular optimization.

	default:
		// We are in a network comprised of less than half of the known nodes.
		// Attempt to connect to an unconnected one to desegregate the network.

		// Copy opts as we are going to make changes.
		opts = opts.Copy()
		opts.NoDefaults = true
		opts.Regard = StateNone
		opts.Disregard = StateSummaryDisregard | StateReachable

		// Iterate over all Pins to find any matching Pin.
		desegregateWith := make([]*Pin, 0, len(m.all)-reachable)
		matcher := opts.Matcher(TransitHub, m.intel)
		for _, pin := range m.all {
			if matcher(pin) {
				desegregateWith = append(desegregateWith, pin)
			}
		}

		// Sort by lowest connection cost.
		sort.Sort(sortByLowestMeasuredCost(desegregateWith))

		// Build desegration optimization.
		result.Purpose = OptimizePurposeDesegregate
		result.Approach = []string{"Attempt to desegregate network by connection to an unreachable Hub."}
		result.MaxConnect = 1
		result.addSuggested("desegregate", desegregateWith...)

		// Record desegregation attempt.
		m.lastDesegrationAttempt = time.Now()

		return true
	}

	return false
}

func (m *Map) optimizeForLowestCost(result *OptimizationResult, max int) {
	// Add approach.
	result.addApproach(fmt.Sprintf("Connect to best (lowest cost) %d Hubs globally.", max))

	// Sort by lowest cost.
	sort.Sort(sortByLowestMeasuredCost(m.regardedPins))

	// Add to suggested pins.
	if len(m.regardedPins) <= max {
		result.addSuggested("best globally", m.regardedPins...)
	} else {
		result.addSuggested("best globally", m.regardedPins[:max]...)
	}
}

func (m *Map) optimizeForDistanceConstraint(result *OptimizationResult, max int) { //nolint:unused // TODO: Likely to be used again.
	// Add approach.
	result.addApproach(fmt.Sprintf("Satisfy max hop constraint of %d globally.", optimizationHopDistanceTarget))

	for range max {
		// Sort by lowest cost.
		sort.Sort(sortBySuggestedHopDistanceAndLowestMeasuredCost(m.regardedPins))

		// Return when all regarded Pins are within the distance constraint.
		if m.regardedPins[0].analysis.SuggestedHopDistance <= optimizationHopDistanceTarget {
			return
		}

		// If not, suggest a connection to the best match.
		result.addSuggested("satisfy global hop constraint", m.regardedPins[0])
	}
}
