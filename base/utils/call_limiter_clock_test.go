package utils

import (
	"testing"
	"time"
)

func TestLimiterWait(t *testing.T) {
	t.Parallel()

	pause := 100 * time.Millisecond
	tests := []struct {
		name     string
		pause    time.Duration
		lastExec time.Time
		min, max time.Duration
	}{
		{"no pause", 0, time.Now(), 0, 0},
		{"never executed", pause, time.Time{}, 0, 0},
		{"long ago", pause, time.Now().Add(-time.Hour), 0, 0},
		{"just executed", pause, time.Now(), pause - 20*time.Millisecond, pause},
		// Wall-clock time in the future, as after the system clock was set back.
		{"clock set back by days", pause, time.Now().Round(0).Add(96 * time.Hour), pause, pause},
	}
	for _, tc := range tests {
		got := limiterWait(tc.pause, tc.lastExec)
		if got < tc.min || got > tc.max {
			t.Errorf("%s: wait %v, want between %v and %v", tc.name, got, tc.min, tc.max)
		}
	}
}

// TestCallLimiterClockSetBack checks that a lastExec in the future (clock set
// back, no monotonic reading) delays the next call by at most one pause.
func TestCallLimiterClockSetBack(t *testing.T) {
	t.Parallel()

	pause := 20 * time.Millisecond
	future := time.Now().Round(0).Add(48 * time.Hour)

	l1 := NewCallLimiter(pause)
	l1.lastExec = future
	l2 := NewCallLimiter2(pause)
	l2.lastExec = future

	for name, do := range map[string]func(func()){"CallLimiter": l1.Do, "CallLimiter2": l2.Do} {
		done := make(chan struct{})
		go do(func() { close(done) })
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: call blocked after the clock was set back", name)
		}
	}
}
