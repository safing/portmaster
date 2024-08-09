package mgr

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerMgrDelay(t *testing.T) {
	t.Parallel()

	m := New("DelayTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that will after 1 second.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Delay(1 * time.Second)

	// Check if value is set after 1 second and not before or after.
	iterations := 0
	for !value.Load() {
		iterations++
		time.Sleep(10 * time.Millisecond)
	}

	// 5% difference is acceptable since time.Sleep can't be perfect and it may very on different computers.
	if iterations < 95 || iterations > 105 {
		t.Errorf("WorkerMgr did not delay for a whole second it=%d", iterations)
	}
}

func TestWorkerMgrRepeat(t *testing.T) {
	t.Parallel()

	m := New("RepeatTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that should repeat every 100 milliseconds.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Repeat(100 * time.Millisecond)

	// Check 10 consecutive runs they should be delayed for around 100 milliseconds each.
	for range 10 {
		iterations := 0
		for !value.Load() {
			iterations++
			time.Sleep(10 * time.Millisecond)
		}

		// 10% difference is acceptable at this scale since time.Sleep can't be perfect and it may very on different computers.
		if iterations < 9 || iterations > 11 {
			t.Errorf("Worker was not delayed for a 100 milliseconds it=%d", iterations)
			return
		}
		// Reset value
		value.Store(false)
	}
}

func TestWorkerMgrDelayAndRepeat(t *testing.T) { //nolint:dupl
	t.Parallel()

	m := New("DelayAndRepeatTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that should delay for 1 second and then repeat every 100 milliseconds.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Delay(1 * time.Second).Repeat(100 * time.Millisecond)

	iterations := 0
	for !value.Load() {
		iterations++
		time.Sleep(10 * time.Millisecond)
	}

	// 5% difference is acceptable since time.Sleep can't be perfect and it may very on different computers.
	if iterations < 95 || iterations > 105 {
		t.Errorf("WorkerMgr did not delay for a whole second it=%d", iterations)
	}

	// Reset value
	value.Store(false)

	// Check 10 consecutive runs they should be delayed for around 100 milliseconds each.
	for range 10 {
		iterations = 0
		for !value.Load() {
			iterations++
			time.Sleep(10 * time.Millisecond)
		}

		// 10% difference is acceptable at this scale since time.Sleep can't be perfect and it may very on different computers.
		if iterations < 9 || iterations > 11 {
			t.Errorf("Worker was not delayed for a 100 milliseconds it=%d", iterations)
			return
		}
		// Reset value
		value.Store(false)
	}
}

func TestWorkerMgrRepeatAndDelay(t *testing.T) { //nolint:dupl
	t.Parallel()

	m := New("RepeatAndDelayTest")

	value := atomic.Bool{}
	value.Store(false)

	// Create a task that should delay for 1 second and then repeat every 100 milliseconds but with reverse command order.
	m.NewWorkerMgr("test", func(w *WorkerCtx) error {
		value.Store(true)
		return nil
	}, nil).Repeat(100 * time.Millisecond).Delay(1 * time.Second)

	iterations := 0
	for !value.Load() {
		iterations++
		time.Sleep(10 * time.Millisecond)
	}

	// 5% difference is acceptable since time.Sleep can't be perfect and it may very on different computers.
	if iterations < 95 || iterations > 105 {
		t.Errorf("WorkerMgr did not delay for a whole second it=%d", iterations)
	}
	// Reset value
	value.Store(false)

	// Check 10 consecutive runs they should be delayed for around 100 milliseconds each.
	for range 10 {
		iterations := 0
		for !value.Load() {
			iterations++
			time.Sleep(10 * time.Millisecond)
		}

		// 10% difference is acceptable at this scale since time.Sleep can't be perfect and it may very on different computers.
		if iterations < 9 || iterations > 11 {
			t.Errorf("Worker was not delayed for a 100 milliseconds it=%d", iterations)
			return
		}
		// Reset value
		value.Store(false)
	}
}
