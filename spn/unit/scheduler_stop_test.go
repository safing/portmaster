package unit

import (
	"testing"
	"time"

	"github.com/safing/portmaster/service/mgr"
)

// TestSchedulerStopReleasesWaiters checks that units that are waiting for a
// slot when the scheduler stops are released, instead of blocking forever.
func TestSchedulerStopReleasesWaiters(t *testing.T) {
	t.Parallel()

	// Use long slots, so that the test can act within a half-slot.
	s := NewScheduler(&SchedulerConfig{
		SlotDuration:       2 * time.Second,
		MinSlotPace:        10,
		WorkSlotPercentage: 0.9,
	})

	// Create some units, so that the first work slot gives a clearance that
	// differs from the initial clearance.
	for range 5 {
		s.NewUnit().Finish()
	}
	initialClearance := s.clearanceUpTo.Load()

	m := mgr.New("unit-test")
	defer m.Cancel()
	schedulerDone := make(chan struct{})
	m.Go("scheduler", func(w *mgr.WorkerCtx) error {
		defer close(schedulerDone)
		return s.SlotScheduler(w)
	})

	// Wait for the first (work) half-slot to start.
	deadline := time.Now().Add(5 * time.Second)
	for s.clearanceUpTo.Load() == initialClearance {
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not start first slot")
		}
		time.Sleep(time.Millisecond)
	}

	// We are now at the start of the work half-slot. The next tick will be the
	// catch-up half-slot. Create a unit beyond the current clearance.
	var u *Unit
	for range 50 {
		u = s.NewUnit()
	}
	if u.id <= s.clearanceUpTo.Load() {
		t.Fatal("test unit unexpectedly has clearance")
	}
	waiterDone := make(chan struct{})
	go func() {
		u.WaitForSlot()
		close(waiterDone)
	}()
	// Give the waiter time to block on the slot signal.
	time.Sleep(50 * time.Millisecond)

	// Stop the scheduler. It will stop at the catch-up half-slot tick, which
	// does not announce a new slot.
	s.Stop()
	select {
	case <-schedulerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop")
	}

	// The waiting unit must be released.
	select {
	case <-waiterDone:
	case <-time.After(3 * time.Second):
		t.Fatal("unit waiting for slot was not released when the scheduler stopped")
	}

	// Units waiting after the scheduler stopped must not block either.
	lateDone := make(chan struct{})
	go func() {
		s.NewUnit().WaitForSlot()
		close(lateDone)
	}()
	select {
	case <-lateDone:
	case <-time.After(3 * time.Second):
		t.Fatal("unit waiting for slot after scheduler stopped was blocked")
	}
}
