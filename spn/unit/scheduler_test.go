package unit

import (
	"testing"

	"github.com/safing/portmaster/service/mgr"
)

func BenchmarkScheduler(b *testing.B) {
	workers := 10

	// Create and start scheduler.
	s := NewScheduler(&SchedulerConfig{})
	m := mgr.New("unit-test")
	m.Go("test", func(ctx *mgr.WorkerCtx) error {
		err := s.SlotScheduler(ctx)
		if err != nil {
			panic(err)
		}
		return nil
	})
	defer m.Cancel()

	// Init control structures.
	done := make(chan struct{})
	finishedCh := make(chan struct{})

	// Start workers.
	for range workers {
		go func() {
			for {
				u := s.NewUnit()
				u.WaitForSlot()
				u.Finish()
				select {
				case finishedCh <- struct{}{}:
				case <-done:
					return
				}
			}
		}()
	}

	// Start benchmark.
	b.ResetTimer()
	for range b.N {
		<-finishedCh
	}
	b.StopTimer()

	// Cleanup.
	close(done)
}
