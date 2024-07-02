package navigator

import (
	"sort"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/terminal"
)

// Measurements Configuration.
const (
	NavigatorMeasurementTTLDefault    = 4 * time.Hour
	NavigatorMeasurementTTLByCostBase = 6 * time.Minute
	NavigatorMeasurementTTLByCostMin  = 4 * time.Hour
	NavigatorMeasurementTTLByCostMax  = 50 * time.Hour

	// With a base TTL of 3m, this leads to:
	// 20c     -> 2h -> raised to 4h.
	// 50c     -> 5h
	// 100c    -> 10h
	// 1000c   -> 100h -> capped to 50h.
)

func (m *Map) measureHubs(wc *mgr.WorkerCtx) error {
	if home, _ := m.GetHome(); home == nil {
		log.Debug("spn/navigator: skipping measuring, no home hub set")
		return nil
	}

	var unknownErrCnt int
	matcher := m.DefaultOptions().Transit.Matcher(m.GetIntel())

	// Get list and sort in order to check near/low-cost hubs earlier.
	list := m.pinList(true)
	sort.Sort(sortByLowestMeasuredCost(list))

	// Find first pin where any measurement has expired.
	for _, pin := range list {
		// Check if measuring is enabled.
		if pin.measurements == nil {
			continue
		}

		// Check if Pin is regarded.
		if !matcher(pin) {
			continue
		}

		// Calculate dynamic TTL.
		var checkWithTTL time.Duration
		if pin.HopDistance == 2 { // Hub is directly connected.
			checkWithTTL = calculateMeasurementTTLByCost(
				pin.measurements.GetCalculatedCost(),
				docks.CraneMeasurementTTLByCostBase,
				docks.CraneMeasurementTTLByCostMin,
				docks.CraneMeasurementTTLByCostMax,
			)
		} else {
			checkWithTTL = calculateMeasurementTTLByCost(
				pin.measurements.GetCalculatedCost(),
				NavigatorMeasurementTTLByCostBase,
				NavigatorMeasurementTTLByCostMin,
				NavigatorMeasurementTTLByCostMax,
			)
		}

		// Check if we have measured the pin within the TTL.
		if !pin.measurements.Expired(checkWithTTL) {
			continue
		}

		// Measure connection.
		tErr := docks.MeasureHub(wc.Ctx(), pin.Hub, checkWithTTL)

		// Independent of outcome, recalculate the cost.
		latency, _ := pin.measurements.GetLatency()
		capacity, _ := pin.measurements.GetCapacity()
		calculatedCost := CalculateLaneCost(latency, capacity)
		pin.measurements.SetCalculatedCost(calculatedCost)
		// Log result.
		log.Infof(
			"spn/navigator: updated measurements for connection to %s: %s %.2fMbit/s %.2fc",
			pin.Hub,
			latency,
			float64(capacity)/1000000,
			calculatedCost,
		)

		switch {
		case tErr.IsOK():
			// All good, continue.

		case tErr.Is(terminal.ErrTryAgainLater):
			if tErr.IsExternal() {
				// Remote is measuring, just continue with next.
				log.Debugf("spn/navigator: remote %s is measuring, continuing with next", pin.Hub)
			} else {
				// We are measuring, abort and restart measuring again later.
				log.Debugf("spn/navigator: postponing measuring because we are currently engaged in measuring")
				return nil
			}

		default:
			log.Warningf("spn/navigator: failed to measure connection to %s: %s", pin.Hub, tErr)
			unknownErrCnt++
			if unknownErrCnt >= 3 {
				log.Warningf("spn/navigator: postponing measuring task because of multiple errors")
				return nil
			}
		}
	}

	return nil
}

// SaveMeasuredHubs saves all Hubs that have unsaved measurements.
func (m *Map) SaveMeasuredHubs() {
	m.RLock()
	defer m.RUnlock()

	for _, pin := range m.all {
		if !pin.measurements.IsPersisted() {
			if err := pin.Hub.Save(); err != nil {
				log.Warningf("spn/navigator: failed to save Hub %s to persist measurements: %s", pin.Hub, err)
			}
		}
	}
}

func calculateMeasurementTTLByCost(cost float32, base, min, max time.Duration) time.Duration {
	calculated := time.Duration(cost) * base
	switch {
	case calculated < min:
		return min
	case calculated > max:
		return max
	default:
		return calculated
	}
}
