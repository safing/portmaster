package compat

import (
	"sync"
	"testing"
)

// TestSelfcheckFailsConcurrentReset checks that resetting the self-check state
// (as stop() does) is safe while the self-check task records failures.
// Run with -race.
func TestSelfcheckFailsConcurrentReset(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1000 {
			recordSelfcheckFailure()
		}
	}()
	go func() {
		defer wg.Done()
		for range 1000 {
			resetSelfCheckState()
		}
	}()
	wg.Wait()
	resetSelfCheckState()

	if n := selfcheckFails.Load(); n != 0 {
		t.Fatalf("expected 0 fails after reset, got %d", n)
	}
}
