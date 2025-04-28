package utils

import (
	"sync"
	"sync/atomic"
	"time"
)

// CallLimiter2 bundles concurrent calls and optionally limits how fast a function is called.
type CallLimiter2 struct {
	pause time.Duration

	slot     atomic.Int64
	slotWait sync.RWMutex

	executing atomic.Bool
	lastExec  time.Time
}

// NewCallLimiter2 returns a new call limiter.
// Set minPause to zero to disable the minimum pause between calls.
func NewCallLimiter2(minPause time.Duration) *CallLimiter2 {
	return &CallLimiter2{
		pause: minPause,
	}
}

// Do executes the given function.
// All concurrent calls to Do are bundled and return when f() finishes.
// Waits until the minimum pause is over before executing f() again.
func (l *CallLimiter2) Do(f func()) {
	// Get ticket number.
	slot := l.slot.Load()

	// Check if we can execute.
	if l.executing.CompareAndSwap(false, true) {
		// Make others wait.
		l.slotWait.Lock()
		defer l.slotWait.Unlock()

		// Execute and return.
		l.waitAndExec(f)
		return
	}

	// Wait for slot to end and check if slot is done.
	for l.slot.Load() == slot {
		time.Sleep(100 * time.Microsecond)
		l.slotWait.RLock()
		l.slotWait.RUnlock() //nolint:staticcheck
	}
}

func (l *CallLimiter2) waitAndExec(f func()) {
	defer func() {
		// Update last exec time.
		l.lastExec = time.Now().UTC()
		// Enable next execution first.
		l.executing.Store(false)
		// Move to next slot aftewards to prevent wait loops.
		l.slot.Add(1)
	}()

	// Wait for the minimum duration between executions.
	if l.pause > 0 {
		sinceLastExec := time.Since(l.lastExec)
		if sinceLastExec < l.pause {
			time.Sleep(l.pause - sinceLastExec)
		}
	}

	// Execute.
	f()
}
