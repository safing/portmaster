package utils

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tevino/abool"
)

func TestCallLimiter(t *testing.T) {
	t.Parallel()

	pause := 10 * time.Millisecond
	oa := NewCallLimiter(pause)
	executed := abool.New()
	var testWg sync.WaitGroup

	// One execution should gobble up the whole batch.
	// We are doing this without sleep in function, so dummy exec first to trigger first pause.
	oa.Do(func() {})
	// Start
	for range 10 {
		testWg.Add(100)
		for range 100 {
			go func() {
				oa.Do(func() {
					if !executed.SetToIf(false, true) {
						t.Errorf("concurrent execution!")
					}
				})
				testWg.Done()
			}()
		}
		testWg.Wait()
		// Check if function was executed at least once.
		if executed.IsNotSet() {
			t.Errorf("no execution!")
		}
		executed.UnSet() // reset check
	}

	// Wait for pause to reset.
	time.Sleep(pause)

	// Continuous use with re-execution.
	// Choose values so that about 10 executions are expected
	var execs uint32
	testWg.Add(200)
	for range 200 {
		go func() {
			oa.Do(func() {
				atomic.AddUint32(&execs, 1)
				time.Sleep(10 * time.Millisecond)
			})
			testWg.Done()
		}()

		// Start one goroutine every 1ms.
		time.Sleep(1 * time.Millisecond)
	}

	testWg.Wait()
	if execs <= 5 {
		t.Errorf("unexpected low exec count: %d", execs)
	}
	if execs >= 15 {
		t.Errorf("unexpected high exec count: %d", execs)
	}

	// Wait for pause to reset.
	time.Sleep(pause)

	// Check if the limiter correctly handles panics.
	testWg.Add(100)
	for range 100 {
		go func() {
			defer func() {
				_ = recover()
				testWg.Done()
			}()
			oa.Do(func() {
				time.Sleep(1 * time.Millisecond)
				panic("test")
			})
		}()
		time.Sleep(100 * time.Microsecond)
	}
	testWg.Wait()
}
