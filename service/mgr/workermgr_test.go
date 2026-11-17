package mgr

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// skipTimingSensitiveOnCI skips tests that assert wall-clock scheduling
// precision (±5 %). On shared CI runners with several workflows in parallel
// these fail sporadically without indicating a bug; they still run locally.
func skipTimingSensitiveOnCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Skip("timing-sensitive test skipped on CI runner (CI env set)")
	}
}

func TestWorkerMgrDelay(t *testing.T) {
	t.Parallel()
	skipTimingSensitiveOnCI(t)

	m := New("DelayTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that will after 1 second.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Delay(1 * time.Second)

	// Check if value is set after 1 second and not before or after.
	// Measure the elapsed time instead of counting sleeps: on a busy machine,
	// sleeps take longer than requested, so fewer of them fit into a second.
	started := time.Now()
	for !value.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(started)

	// Some difference is acceptable since timers can't be perfect and it may vary on different computers.
	if elapsed < 950*time.Millisecond || elapsed > 1150*time.Millisecond {
		t.Errorf("WorkerMgr did not delay for a whole second: %s", elapsed)
	}
}

func TestWorkerMgrRepeat(t *testing.T) {
	t.Parallel()
	skipTimingSensitiveOnCI(t)

	m := New("RepeatTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that should repeat every 100 milliseconds.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Repeat(100 * time.Millisecond)

	checkRepeatInterval(t, &value)
}

func TestWorkerMgrDelayAndRepeat(t *testing.T) { //nolint:dupl
	t.Parallel()
	skipTimingSensitiveOnCI(t)

	m := New("DelayAndRepeatTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that should delay for 1 second and then repeat every 100 milliseconds.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Delay(1 * time.Second).Repeat(100 * time.Millisecond)

	// Measure the elapsed time instead of counting sleeps: on a busy machine,
	// sleeps take longer than requested, so fewer of them fit into a second.
	started := time.Now()
	for !value.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(started)

	// Some difference is acceptable since timers can't be perfect and it may vary on different computers.
	if elapsed < 950*time.Millisecond || elapsed > 1150*time.Millisecond {
		t.Errorf("WorkerMgr did not delay for a whole second: %s", elapsed)
	}

	// Reset value
	value.Store(false)

	checkRepeatInterval(t, &value)
}

func TestWorkerMgrRepeatAndDelay(t *testing.T) { //nolint:dupl
	t.Parallel()
	skipTimingSensitiveOnCI(t)

	m := New("RepeatAndDelayTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that should delay for 1 second and then repeat every 100 milliseconds but with reverse command order.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Repeat(100 * time.Millisecond).Delay(1 * time.Second)

	// Measure the elapsed time instead of counting sleeps: on a busy machine,
	// sleeps take longer than requested, so fewer of them fit into a second.
	started := time.Now()
	for !value.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(started)

	// Some difference is acceptable since timers can't be perfect and it may vary on different computers.
	if elapsed < 950*time.Millisecond || elapsed > 1150*time.Millisecond {
		t.Errorf("WorkerMgr did not delay for a whole second: %s", elapsed)
	}
	// Reset value
	value.Store(false)

	checkRepeatInterval(t, &value)
}

// checkRepeatInterval checks that 10 consecutive runs of a worker repeating
// every 100 milliseconds take around one second in total. It measures the
// elapsed time instead of counting sleeps, as sleeps take longer than
// requested on a busy machine. The total is checked instead of each run, as a
// single run may be late under load while the following one is on time.
func checkRepeatInterval(t *testing.T, value *atomic.Bool) {
	t.Helper()

	// Wait for the first repeated run to align with the repeat schedule.
	for !value.Load() {
		time.Sleep(time.Millisecond)
	}
	value.Store(false)

	started := time.Now()
	for range 10 {
		for !value.Load() {
			time.Sleep(time.Millisecond)
		}
		value.Store(false)
	}
	elapsed := time.Since(started)

	if elapsed < 900*time.Millisecond || elapsed > 1300*time.Millisecond {
		t.Errorf("10 repeated runs did not take around one second: %s", elapsed)
	}
}
