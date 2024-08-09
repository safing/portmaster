package terminal

import (
	"errors"
	"flag"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/unit"
)

// TerminalModule is the command multiplexing module.
type TerminalModule struct { //nolint:golint
	mgr      *mgr.Manager
	instance instance
}

// Manager returns the module manager.
func (s *TerminalModule) Manager() *mgr.Manager {
	return s.mgr
}

// Start starts the module.
func (s *TerminalModule) Start() error {
	return start()
}

// Stop stops the module.
func (s *TerminalModule) Stop() error {
	return nil
}

var (
	rngFeeder *rng.Feeder = nil

	scheduler *unit.Scheduler

	debugUnitScheduling bool
)

func init() {
	flag.BoolVar(&debugUnitScheduling, "debug-unit-scheduling", false, "enable debug logs of the SPN unit scheduler")
}

func start() error {
	rngFeeder = rng.NewFeeder()

	scheduler = unit.NewScheduler(getSchedulerConfig())
	if debugUnitScheduling {
		// Debug unit leaks.
		scheduler.StartDebugLog()
	}
	module.mgr.Go("msg unit scheduler", scheduler.SlotScheduler)

	lockOpRegistry()

	return registerMetrics()
}

var waitForever chan time.Time

// TimedOut returns a channel that triggers when the timeout is reached.
func TimedOut(timeout time.Duration) <-chan time.Time {
	if timeout == 0 {
		return waitForever
	}
	return time.After(timeout)
}

// StopScheduler stops the unit scheduler.
func StopScheduler() {
	if scheduler != nil {
		scheduler.Stop()
	}
}

func getSchedulerConfig() *unit.SchedulerConfig {
	// Client Scheduler Config.
	if conf.Client() {
		return &unit.SchedulerConfig{
			SlotDuration:            10 * time.Millisecond, // 100 slots per second
			MinSlotPace:             10,                    // 1000pps - Small starting pace for low end devices.
			WorkSlotPercentage:      0.9,                   // 90%
			SlotChangeRatePerStreak: 0.1,                   // 10% - Increase/Decrease quickly.
			StatCycleDuration:       1 * time.Minute,       // Match metrics report cycle.
		}
	}

	// Server Scheduler Config.
	return &unit.SchedulerConfig{
		SlotDuration:            10 * time.Millisecond, // 100 slots per second
		MinSlotPace:             100,                   // 10000pps - Every server should be able to handle this.
		WorkSlotPercentage:      0.7,                   // 70%
		SlotChangeRatePerStreak: 0.05,                  // 5%
		StatCycleDuration:       1 * time.Minute,       // Match metrics report cycle.
	}
}

var (
	module     *TerminalModule
	shimLoaded atomic.Bool
)

// New returns a new Config module.
func New(instance instance) (*TerminalModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("TerminalModule")
	module = &TerminalModule{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface{}
