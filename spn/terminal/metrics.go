package terminal

import (
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

	// Get scheduler config and calculat scaling.
	schedulerConfig := getSchedulerConfig()
	scaleSlotToSecondsFactor := float64(time.Second / schedulerConfig.SlotDuration)

	// Register metrics from scheduler stats.

	_, err = metrics.NewGauge(
		"spn/scheduling/unit/slotpace/max",
		nil,
		metricFromInt(scheduler.GetMaxSlotPace, scaleSlotToSecondsFactor),
		&metrics.Options{
			Name:       "SPN Scheduling Max Slot Pace (scaled to per second)",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/scheduling/unit/slotpace/leveled/max",
		nil,
		metricFromInt(scheduler.GetMaxLeveledSlotPace, scaleSlotToSecondsFactor),
		&metrics.Options{
			Name:       "SPN Scheduling Max Leveled Slot Pace (scaled to per second)",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/scheduling/unit/slotpace/avg",
		nil,
		metricFromInt(scheduler.GetAvgSlotPace, scaleSlotToSecondsFactor),
		&metrics.Options{
			Name:       "SPN Scheduling Avg Slot Pace (scaled to per second)",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/scheduling/unit/life/avg/seconds",
		nil,
		metricFromNanoseconds(scheduler.GetAvgUnitLife),
		&metrics.Options{
			Name:       "SPN Scheduling Avg Unit Life",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/scheduling/unit/workslot/avg/seconds",
		nil,
		metricFromNanoseconds(scheduler.GetAvgWorkSlotDuration),
		&metrics.Options{
			Name:       "SPN Scheduling Avg Work Slot Duration",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/scheduling/unit/catchupslot/avg/seconds",
		nil,
		metricFromNanoseconds(scheduler.GetAvgCatchUpSlotDuration),
		&metrics.Options{
			Name:       "SPN Scheduling Avg Catch-Up Slot Duration",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func metricFromInt(fn func() int64, scaleFactor float64) func() float64 {
	return func() float64 {
		return float64(fn()) * scaleFactor
	}
}

func metricFromNanoseconds(fn func() int64) func() float64 {
	return func() float64 {
		return float64(fn()) / float64(time.Second)
	}
}
