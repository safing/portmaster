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

	// One execution should gobble up the whole batch.
	for range 10 {
		testWg.Add(100)
		for range 100 {
			go func() {
				oa.Do(func() {
					if !executed.SetToIf(false, true) {
						t.Errorf("concurrent execution!")
					}
					time.Sleep(10 * time.Millisecond)
				})
				testWg.Done()
			}()
		}
		testWg.Wait()
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
	if execs <= 8 {
		t.Errorf("unexpected low exec count: %d", execs)
	}
	if execs >= 12 {
		t.Errorf("unexpected high exec count: %d", execs)
	}
}
