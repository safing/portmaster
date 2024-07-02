package docks

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/metrics"
)

var (
	newCranes              *metrics.Counter
	newPublicCranes        *metrics.Counter
	newAuthenticatedCranes *metrics.Counter

	trafficBytesPublicCranes        *metrics.Counter
	trafficBytesAuthenticatedCranes *metrics.Counter
	trafficBytesPrivateCranes       *metrics.Counter

	newExpandOp                  *metrics.Counter
	expandOpDurationHistogram    *metrics.Histogram
	expandOpRelayedDataHistogram *metrics.Histogram

	metricsRegistered = abool.New()
)

func registerMetrics() (err error) {
	// Only register metrics once.
	if !metricsRegistered.SetToIf(false, true) {
		return nil
	}

	// Total Crane Stats.

	newCranes, err = metrics.NewCounter(
		"spn/cranes/total",
		nil,
		&metrics.Options{
			Name:       "SPN New Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	newPublicCranes, err = metrics.NewCounter(
		"spn/cranes/public/total",
		nil,
		&metrics.Options{
			Name:       "SPN New Public Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	newAuthenticatedCranes, err = metrics.NewCounter(
		"spn/cranes/authenticated/total",
		nil,
		&metrics.Options{
			Name:       "SPN New Authenticated Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	// Active Crane Stats.

	_, err = metrics.NewGauge(
		"spn/cranes/active",
		map[string]string{
			"status": "public",
		},
		getActivePublicCranes,
		&metrics.Options{
			Name:       "SPN Active Public Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/cranes/active",
		map[string]string{
			"status": "authenticated",
		},
		getActiveAuthenticatedCranes,
		&metrics.Options{
			Name:       "SPN Active Authenticated Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/cranes/active",
		map[string]string{
			"status": "private",
		},
		getActivePrivateCranes,
		&metrics.Options{
			Name:       "SPN Active Private Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/cranes/active",
		map[string]string{
			"status": "stopping",
		},
		getActiveStoppingCranes,
		&metrics.Options{
			Name:       "SPN Active Stopping Cranes",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	// Crane Traffic Stats.

	trafficBytesPublicCranes, err = metrics.NewCounter(
		"spn/cranes/bytes",
		map[string]string{
			"status": "public",
		},
		&metrics.Options{
			Name:       "SPN Public Crane Traffic",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	trafficBytesAuthenticatedCranes, err = metrics.NewCounter(
		"spn/cranes/bytes",
		map[string]string{
			"status": "authenticated",
		},
		&metrics.Options{
			Name:       "SPN Authenticated Crane Traffic",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	trafficBytesPrivateCranes, err = metrics.NewCounter(
		"spn/cranes/bytes",
		map[string]string{
			"status": "private",
		},
		&metrics.Options{
			Name:       "SPN Private Crane Traffic",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	// Lane Stats.

	_, err = metrics.NewGauge(
		"spn/lanes/latency/avg/seconds",
		nil,
		getAvgLaneLatencyStat,
		&metrics.Options{
			Name:       "SPN Avg Lane Latency",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/lanes/latency/min/seconds",
		nil,
		getMinLaneLatencyStat,
		&metrics.Options{
			Name:       "SPN Min Lane Latency",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/lanes/capacity/avg/bytes",
		nil,
		getAvgLaneCapacityStat,
		&metrics.Options{
			Name:       "SPN Avg Lane Capacity",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/lanes/capacity/max/bytes",
		nil,
		getMaxLaneCapacityStat,
		&metrics.Options{
			Name:       "SPN Max Lane Capacity",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	// Expand Op Stats.

	newExpandOp, err = metrics.NewCounter(
		"spn/op/expand/total",
		nil,
		&metrics.Options{
			Name:       "SPN Total Expand Operations",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/op/expand/active",
		nil,
		getActiveExpandOpsStat,
		&metrics.Options{
			Name:       "SPN Active Expand Operations",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	expandOpDurationHistogram, err = metrics.NewHistogram(
		"spn/op/expand/histogram/duration/seconds",
		nil,
		&metrics.Options{
			Name:       "SPN Expand Operation Duration Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	expandOpRelayedDataHistogram, err = metrics.NewHistogram(
		"spn/op/expand/histogram/traffic/bytes",
		nil,
		&metrics.Options{
			Name:       "SPN Expand Operation Relayed Data Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	return err
}

func getActiveExpandOpsStat() float64 {
	return float64(atomic.LoadInt64(activeExpandOps))
}

var (
	craneStats        *craneGauges
	craneStatsExpires time.Time
	craneStatsLock    sync.Mutex
	craneStatsTTL     = 55 * time.Second
)

type craneGauges struct {
	publicActive        float64
	authenticatedActive float64
	privateActive       float64
	stoppingActive      float64

	laneLatencyAvg  float64
	laneLatencyMin  float64
	laneCapacityAvg float64
	laneCapacityMax float64
}

func getActivePublicCranes() float64        { return getCraneStats().publicActive }
func getActiveAuthenticatedCranes() float64 { return getCraneStats().authenticatedActive }
func getActivePrivateCranes() float64       { return getCraneStats().privateActive }
func getActiveStoppingCranes() float64      { return getCraneStats().stoppingActive }
func getAvgLaneLatencyStat() float64        { return getCraneStats().laneLatencyAvg }
func getMinLaneLatencyStat() float64        { return getCraneStats().laneLatencyMin }
func getAvgLaneCapacityStat() float64       { return getCraneStats().laneCapacityAvg }
func getMaxLaneCapacityStat() float64       { return getCraneStats().laneCapacityMax }

func getCraneStats() *craneGauges {
	craneStatsLock.Lock()
	defer craneStatsLock.Unlock()

	// Return cache if still valid.
	if time.Now().Before(craneStatsExpires) {
		return craneStats
	}

	// Refresh.
	craneStats = &craneGauges{}
	var laneStatCnt float64
	for _, crane := range getAllCranes() {
		switch {
		case crane.Stopped():
			continue
		case crane.IsStopping():
			craneStats.stoppingActive++
			continue
		case crane.Public():
			craneStats.publicActive++
		case crane.Authenticated():
			craneStats.authenticatedActive++
			continue
		default:
			craneStats.privateActive++
			continue
		}

		// Get lane stats.
		if crane.ConnectedHub == nil {
			continue
		}
		measurements := crane.ConnectedHub.GetMeasurements()
		laneLatency, _ := measurements.GetLatency()
		if laneLatency == 0 {
			continue
		}
		laneCapacity, _ := measurements.GetCapacity()
		if laneCapacity == 0 {
			continue
		}

		// Only use data if both latency and capacity is available.
		laneStatCnt++

		// Convert to base unit: seconds.
		latency := laneLatency.Seconds()
		// Add to avg and set min if lower.
		craneStats.laneLatencyAvg += latency
		if craneStats.laneLatencyMin > latency || craneStats.laneLatencyMin == 0 {
			craneStats.laneLatencyMin = latency
		}

		// Convert in base unit: bytes.
		capacity := float64(laneCapacity) / 8
		// Add to avg and set max if higher.
		craneStats.laneCapacityAvg += capacity
		if craneStats.laneCapacityMax < capacity {
			craneStats.laneCapacityMax = capacity
		}
	}

	// Create averages.
	if laneStatCnt > 0 {
		craneStats.laneLatencyAvg /= laneStatCnt
		craneStats.laneCapacityAvg /= laneStatCnt
	}

	craneStatsExpires = time.Now().Add(craneStatsTTL)
	return craneStats
}

func (crane *Crane) submitCraneTrafficStats(bytes int) {
	switch {
	case crane.Stopped():
		return
	case crane.Public():
		trafficBytesPublicCranes.Add(bytes)
	case crane.Authenticated():
		trafficBytesAuthenticatedCranes.Add(bytes)
	default:
		trafficBytesPrivateCranes.Add(bytes)
	}
}
