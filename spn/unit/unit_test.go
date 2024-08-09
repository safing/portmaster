package unit

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/service/mgr"
)

func TestUnit(t *testing.T) { //nolint:paralleltest
	// Ignore deprectation, as the given alternative is not safe for concurrent use.
	// The global rand methods use a locked seed, which is not available from outside.
	rand.Seed(time.Now().UnixNano()) //nolint

	size := 1000000
	workers := 100

	m := mgr.New("unit-test")
	// Create and start scheduler.
	s := NewScheduler(&SchedulerConfig{})
	s.StartDebugLog()
	// ctx, cancel := context.WithCancel(context.Background())
	m.Go("test", func(w *mgr.WorkerCtx) error {
		err := s.SlotScheduler(w)
		if err != nil {
			panic(err)
		}
		return nil
	})
	defer m.Cancel()

	// Create 10 workers.
	var wg sync.WaitGroup
	wg.Add(workers)
	sizePerWorker := size / workers
	for range workers {
		go func() {
			for range sizePerWorker {
				u := s.NewUnit()

				// Make 1% high priority.
				if rand.Int()%100 == 0 { //nolint:gosec // This is a test.
					u.MakeHighPriority()
				}

				u.WaitForSlot()
				time.Sleep(10 * time.Microsecond)
				u.Finish()
			}
			wg.Done()
		}()
	}

	// Wait for workers to finish.
	wg.Wait()

	// Wait for two slot durations for values to update.
	time.Sleep(s.config.SlotDuration * 2)

	// Print current state.
	s.cycleStats()
	fmt.Printf(`scheduler state:
		currentUnitID = %d
		slotPace = %d
		clearanceUpTo = %d
		finished = %d
		maxPace = %d
		maxLeveledPace = %d
		avgPace = %d
		avgUnitLife = %s
		avgWorkSlot = %s
		avgCatchUpSlot = %s
`,
		s.currentUnitID.Load(),
		s.slotPace.Load(),
		s.clearanceUpTo.Load(),
		s.finished.Load(),
		s.GetMaxSlotPace(),
		s.GetMaxLeveledSlotPace(),
		s.GetAvgSlotPace(),
		time.Duration(s.GetAvgUnitLife()),
		time.Duration(s.GetAvgWorkSlotDuration()),
		time.Duration(s.GetAvgCatchUpSlotDuration()),
	)

	// Check if everything seems good.
	assert.Equal(t, size, int(s.currentUnitID.Load()), "currentUnitID must match size")
	assert.GreaterOrEqual(
		t,
		int(s.clearanceUpTo.Load()),
		size+int(float64(s.config.MinSlotPace)*s.config.SlotChangeRatePerStreak),
		"clearanceUpTo must be at least size+minSlotPace",
	)

	// Shutdown
	m.Cancel()
	time.Sleep(s.config.SlotDuration * 10)

	// Check if scheduler shut down correctly.
	assert.Equal(t, math.MaxInt64-math.MaxInt32, int(s.clearanceUpTo.Load()), "clearance must be near MaxInt64")
}
