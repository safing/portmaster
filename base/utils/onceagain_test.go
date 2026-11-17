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

	testWg.Wait()
	// Allow for scheduling jitter on loaded machines (eg. CI running many
	// test packages at once), same bounds as the call limiter test.
	if execs <= 5 {
		t.Errorf("unexpected low exec count: %d", execs)
	}
	if execs >= 15 {
		t.Errorf("unexpected high exec count: %d", execs)
	}
}
