package utils

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tevino/abool"
)

func TestOnceAgain(t *testing.T) {
	t.Parallel()

	oa := OnceAgain{}
	executed := abool.New()
	var testWg sync.WaitGroup
	var inFlight atomic.Int32

	// One execution should gobble up the whole batch.
	for range 10 {
		testWg.Add(100)
		for range 100 {
			go func() {
				oa.Do(func() {
					// Check for actual concurrent execution. Multiple sequential
					// executions per batch are allowed, as goroutines might not
					// all be started in time (eg. with the race detector).
					if inFlight.Add(1) != 1 {
						t.Errorf("concurrent execution!")
					}
					executed.Set()
					time.Sleep(10 * time.Millisecond)
					inFlight.Add(-1)
				})
				testWg.Done()
			}()
		}
		testWg.Wait()
		if executed.IsNotSet() {
			t.Errorf("no execution!")
		}
		executed.UnSet() // reset check
	}

	// Continuous use with re-execution.
	// Choose values so that about 10 executions are expected
	var execs uint32
	testWg.Add(100)
	started := time.Now()
	for range 100 {
		go func() {
			oa.Do(func() {
				atomic.AddUint32(&execs, 1)
				time.Sleep(10 * time.Millisecond)
			})
			testWg.Done()
		}()

		time.Sleep(1 * time.Millisecond)
	}

	spawned := time.Since(started)
	testWg.Wait()
	// Executions take at least 10ms and do not overlap, so at most one
	// execution fits into every 10ms of spawning, plus one running and one
	// queued at the end. Sleeps may take longer than requested (eg. on
	// Windows or on loaded machines), so derive the bound from the time
	// spawning actually took.
	maxExecs := uint32(spawned/(10*time.Millisecond)) + 2 //nolint:gosec // Spawning takes seconds at most.
	if execs <= 5 {
		t.Errorf("unexpected low exec count: %d", execs)
	}
	if execs > maxExecs {
		t.Errorf("unexpected high exec count: %d (max %d for %s of spawning)", execs, maxExecs, spawned)
	}
}
