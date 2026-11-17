package terminal

import (
	"sync"
	"testing"

	"github.com/safing/structures/varint"
)

// TestCounterOpStopWhileDelivering checks that stopping a counter operation
// while it is still receiving counters does not race on the counters.
// Run with -race.
func TestCounterOpStopWhileDelivering(t *testing.T) {
	t.Parallel()

	op := &CounterOp{
		opts: &CounterOpts{
			ClientCountTo: 1_000_000,
			ServerCountTo: 1_000_000,
		},
	}
	op.wg.Add(1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := uint64(1); i <= 1000; i++ {
			tErr := op.Deliver(NewMsg(varint.Pack64(i)))
			if tErr != nil {
				t.Errorf("failed to deliver counter %d: %s", i, tErr)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		_ = op.HandleStop(nil)
	}()
	wg.Wait()
	op.Wait()

	if op.Error == nil {
		t.Error("expected unfinished counting error")
	}
}
