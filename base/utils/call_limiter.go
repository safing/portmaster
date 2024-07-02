package utils

import (
	"sync"
	"sync/atomic"
	"time"
)

// CallLimiter bundles concurrent calls and optionally limits how fast a function is called.
type CallLimiter struct {
	pause time.Duration

	inLock   sync.Mutex
	lastExec time.Time

	waiters atomic.Int32
	outLock sync.Mutex
}

// NewCallLimiter returns a new call limiter.
// Set minPause to zero to disable the minimum pause between calls.
func NewCallLimiter(minPause time.Duration) *CallLimiter {
	return &CallLimiter{
		pause: minPause,
	}
}

// Do executes the given function.
// All concurrent calls to Do are bundled and return when f() finishes.
// Waits until the minimum pause is over before executing f() again.
func (l *CallLimiter) Do(f func()) {
	// Wait for the previous waiters to exit.
	l.inLock.Lock()

	// Defer final unlock to safeguard from panics.
	defer func() {
		// Execution is finished - leave.
		// If we are the last waiter, let the next batch in.
		if l.waiters.Add(-1) == 0 {
			l.inLock.Unlock()
		}
	}()

	// Check if we are the first waiter.
	if l.waiters.Add(1) == 1 {
		// Take the lead on this execution run.
		l.lead(f)
	} else {
		// We are not the first waiter, let others in.
		l.inLock.Unlock()
	}

	// Wait for execution to complete.
	l.outLock.Lock()
	l.outLock.Unlock() //nolint:staticcheck

	// Last statement is in defer above.
}

func (l *CallLimiter) lead(f func()) {
	// Make all others wait while we execute the function.
	l.outLock.Lock()

	// Unlock in lock until execution is finished.
	l.inLock.Unlock()

	// Transition from out lock to in lock when done.
	defer func() {
		// Update last execution time.
		l.lastExec = time.Now().UTC()
		// Stop newcomers from waiting on previous execution.
		l.inLock.Lock()
		// Allow waiters to leave.
		l.outLock.Unlock()
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
