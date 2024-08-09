package unit

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/mgr"
)

const (
	defaultSlotDuration = 10 * time.Millisecond // 100 slots per second
	defaultMinSlotPace  = 100                   // 10 000 pps

	defaultWorkSlotPercentage      = 0.7  // 70%
	defaultSlotChangeRatePerStreak = 0.02 // 2%

	defaultStatCycleDuration = 1 * time.Minute
)

// Scheduler creates and schedules units.
// Must be created using NewScheduler().
type Scheduler struct { //nolint:maligned
	// Configuration.
	config SchedulerConfig

	// Units IDs Limit / Thresholds.

	// currentUnitID holds the last assigned Unit ID.
	currentUnitID atomic.Int64
	// clearanceUpTo holds the current threshold up to which Unit ID Units may be processed.
	clearanceUpTo atomic.Int64
	// slotPace holds the current pace. This is the base value for clearance
	// calculation, not the value of the current cleared Units itself.
	slotPace atomic.Int64
	// finished holds the amount of units that were finished within the current slot.
	finished atomic.Int64

	// Slot management.
	slotSignalA      chan struct{}
	slotSignalB      chan struct{}
	slotSignalSwitch bool
	slotSignalsLock  sync.RWMutex

	stopping     abool.AtomicBool
	unitDebugger *UnitDebugger

	// Stats.
	stats struct {
		// Working Values.
		progress struct {
			maxPace           atomic.Int64
			maxLeveledPace    atomic.Int64
			avgPaceSum        atomic.Int64
			avgPaceCnt        atomic.Int64
			avgUnitLifeSum    atomic.Int64
			avgUnitLifeCnt    atomic.Int64
			avgWorkSlotSum    atomic.Int64
			avgWorkSlotCnt    atomic.Int64
			avgCatchUpSlotSum atomic.Int64
			avgCatchUpSlotCnt atomic.Int64
		}

		// Calculated Values.
		current struct {
			maxPace        atomic.Int64
			maxLeveledPace atomic.Int64
			avgPace        atomic.Int64
			avgUnitLife    atomic.Int64
			avgWorkSlot    atomic.Int64
			avgCatchUpSlot atomic.Int64
		}
	}
}

// SchedulerConfig holds scheduler configuration.
type SchedulerConfig struct {
	// SlotDuration defines the duration of one slot.
	SlotDuration time.Duration

	// MinSlotPace defines the minimum slot pace.
	// The slot pace will never fall below this value.
	MinSlotPace int64

	// WorkSlotPercentage defines the how much of a slot should be scheduled with work.
	// The remainder is for catching up and breathing room for other tasks.
	// Must be between 55% (0.55) and 95% (0.95).
	// The default value is 0.7 (70%).
	WorkSlotPercentage float64

	// SlotChangeRatePerStreak defines how many percent (0-1) the slot pace
	// should change per streak.
	// Is enforced to be able to change the minimum slot pace by at least 1.
	// The default value is 0.02 (2%).
	SlotChangeRatePerStreak float64

	// StatCycleDuration defines how often stats are calculated.
	// The default value is 1 minute.
	StatCycleDuration time.Duration
}

// NewScheduler returns a new scheduler.
func NewScheduler(config *SchedulerConfig) *Scheduler {
	// Fallback to empty config if none is given.
	if config == nil {
		config = &SchedulerConfig{}
	}

	// Create new scheduler.
	s := &Scheduler{
		config:      *config,
		slotSignalA: make(chan struct{}),
		slotSignalB: make(chan struct{}),
	}

	// Fill in defaults.
	if s.config.SlotDuration == 0 {
		s.config.SlotDuration = defaultSlotDuration
	}
	if s.config.MinSlotPace == 0 {
		s.config.MinSlotPace = defaultMinSlotPace
	}
	if s.config.WorkSlotPercentage == 0 {
		s.config.WorkSlotPercentage = defaultWorkSlotPercentage
	}
	if s.config.SlotChangeRatePerStreak == 0 {
		s.config.SlotChangeRatePerStreak = defaultSlotChangeRatePerStreak
	}
	if s.config.StatCycleDuration == 0 {
		s.config.StatCycleDuration = defaultStatCycleDuration
	}

	// Check boundaries of WorkSlotPercentage.
	switch {
	case s.config.WorkSlotPercentage < 0.55:
		s.config.WorkSlotPercentage = 0.55
	case s.config.WorkSlotPercentage > 0.95:
		s.config.WorkSlotPercentage = 0.95
	}

	// The slot change rate must be able to change the slot pace by at least 1.
	if s.config.SlotChangeRatePerStreak < (1 / float64(s.config.MinSlotPace)) {
		s.config.SlotChangeRatePerStreak = (1 / float64(s.config.MinSlotPace))

		// Debug logging:
		// fmt.Printf("--- increased SlotChangeRatePerStreak to %f\n", s.config.SlotChangeRatePerStreak)
	}

	// Initialize scheduler fields.
	s.clearanceUpTo.Store(s.config.MinSlotPace)
	s.slotPace.Store(s.config.MinSlotPace)

	return s
}

func (s *Scheduler) nextSlotSignal() chan struct{} {
	s.slotSignalsLock.RLock()
	defer s.slotSignalsLock.RUnlock()

	if s.slotSignalSwitch {
		return s.slotSignalA
	}
	return s.slotSignalB
}

func (s *Scheduler) announceNextSlot() {
	s.slotSignalsLock.Lock()
	defer s.slotSignalsLock.Unlock()

	// Close new slot signal and refresh previous one.
	if s.slotSignalSwitch {
		close(s.slotSignalA)
		s.slotSignalB = make(chan struct{})
	} else {
		close(s.slotSignalB)
		s.slotSignalA = make(chan struct{})
	}

	// Switch to next slot.
	s.slotSignalSwitch = !s.slotSignalSwitch
}

// SlotScheduler manages the slot and schedules units.
// Must only be started once.
func (s *Scheduler) SlotScheduler(ctx *mgr.WorkerCtx) error {
	// Start slot ticker.
	ticker := time.NewTicker(s.config.SlotDuration / 2)
	defer ticker.Stop()

	// Give clearance to all when stopping.
	defer s.clearanceUpTo.Store(math.MaxInt64 - math.MaxInt32)

	var (
		halfSlotID        uint64
		halfSlotStartedAt = time.Now()
		halfSlotEndedAt   time.Time
		halfSlotDuration  = float64(s.config.SlotDuration / 2)

		increaseStreak float64
		decreaseStreak float64
		oneStreaks     int

		cycleStatsAt = uint64(s.config.StatCycleDuration / (s.config.SlotDuration / 2))
	)

	for range ticker.C {
		halfSlotEndedAt = time.Now()

		switch {
		case halfSlotID%2 == 0:

			// First Half-Slot: Work Slot

			// Calculate time taken in previous slot.
			catchUpSlotDuration := halfSlotEndedAt.Sub(halfSlotStartedAt).Nanoseconds()

			// Add current slot duration to avg calculation.
			s.stats.progress.avgCatchUpSlotCnt.Add(1)
			if s.stats.progress.avgCatchUpSlotSum.Add(catchUpSlotDuration) < 0 {
				// Reset if we wrap.
				s.stats.progress.avgCatchUpSlotCnt.Store(1)
				s.stats.progress.avgCatchUpSlotSum.Store(catchUpSlotDuration)
			}

			// Reset slot counters.
			s.finished.Store(0)

			// Raise clearance according
			s.clearanceUpTo.Store(
				s.currentUnitID.Load() +
					int64(
						float64(s.slotPace.Load())*s.config.WorkSlotPercentage,
					),
			)

			// Announce start of new slot.
			s.announceNextSlot()

		default:

			// Second Half-Slot: Catch-Up Slot

			// Calculate time taken in previous slot.
			workSlotDuration := halfSlotEndedAt.Sub(halfSlotStartedAt).Nanoseconds()

			// Add current slot duration to avg calculation.
			s.stats.progress.avgWorkSlotCnt.Add(1)
			if s.stats.progress.avgWorkSlotSum.Add(workSlotDuration) < 0 {
				// Reset if we wrap.
				s.stats.progress.avgWorkSlotCnt.Store(1)
				s.stats.progress.avgWorkSlotSum.Store(workSlotDuration)
			}

			// Calculate slot duration skew correction, as slots will not run in the
			// exact specified duration.
			slotDurationSkewCorrection := halfSlotDuration / float64(workSlotDuration)

			// Calculate slot pace with performance of first half-slot.
			// Get current slot pace as float64.
			currentSlotPace := float64(s.slotPace.Load())
			// Calculate current raw slot pace.
			newRawSlotPace := float64(s.finished.Load()*2) * slotDurationSkewCorrection

			// Move slot pace in the trending direction.
			if newRawSlotPace >= currentSlotPace {
				// Adjust based on streak.
				increaseStreak++
				decreaseStreak = 0
				s.slotPace.Add(int64(
					currentSlotPace * s.config.SlotChangeRatePerStreak * increaseStreak,
				))

				// Count one-streaks.
				if increaseStreak == 1 {
					oneStreaks++
				} else {
					oneStreaks = 0
				}

				// Debug logging:
				// fmt.Printf("+++ slot pace: %.0f (current raw pace: %.0f, increaseStreak: %.0f, clearanceUpTo: %d)\n", currentSlotPace, newRawSlotPace, increaseStreak, s.clearanceUpTo.Load())
			} else {
				// Adjust based on streak.
				decreaseStreak++
				increaseStreak = 0
				s.slotPace.Add(int64(
					-currentSlotPace * s.config.SlotChangeRatePerStreak * decreaseStreak,
				))

				// Enforce minimum.
				if s.slotPace.Load() < s.config.MinSlotPace {
					s.slotPace.Store(s.config.MinSlotPace)
					decreaseStreak = 0
				}

				// Count one-streaks.
				if decreaseStreak == 1 {
					oneStreaks++
				} else {
					oneStreaks = 0
				}

				// Debug logging:
				// fmt.Printf("--- slot pace: %.0f (current raw pace: %.0f, decreaseStreak: %.0f, clearanceUpTo: %d)\n", currentSlotPace, newRawSlotPace, decreaseStreak, s.clearanceUpTo.Load())
			}

			// Record Stats

			// Add current pace to avg calculation.
			s.stats.progress.avgPaceCnt.Add(1)
			if s.stats.progress.avgPaceSum.Add(s.slotPace.Load()) < 0 {
				// Reset if we wrap.
				s.stats.progress.avgPaceCnt.Store(1)
				s.stats.progress.avgPaceSum.Store(s.slotPace.Load())
			}

			// Check if current pace is new max.
			if s.slotPace.Load() > s.stats.progress.maxPace.Load() {
				s.stats.progress.maxPace.Store(s.slotPace.Load())
			}

			// Check if current pace is new leveled max
			if oneStreaks >= 3 && s.slotPace.Load() > s.stats.progress.maxLeveledPace.Load() {
				s.stats.progress.maxLeveledPace.Store(s.slotPace.Load())
			}
		}
		// Switch to other slot-half.
		halfSlotID++
		halfSlotStartedAt = halfSlotEndedAt

		// Cycle stats after defined time period.
		if halfSlotID%cycleStatsAt == 0 {
			s.cycleStats()
		}

		// Check if we are stopping.
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if s.stopping.IsSet() {
			return nil
		}
	}

	// We should never get here.
	// If we do, trigger a worker restart via the service worker.
	return errors.New("unexpected end of scheduler")
}

// Stop stops the scheduler and gives clearance to all units.
func (s *Scheduler) Stop() {
	s.stopping.Set()
}
