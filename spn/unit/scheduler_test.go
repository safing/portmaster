package unit

import (
	"context"
	"testing"
)

func BenchmarkScheduler(b *testing.B) {
	workers := 10

	// Create and start scheduler.
	s := NewScheduler(&SchedulerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := s.SlotScheduler(ctx)
		if err != nil {
			panic(err)
		}
	}()
	defer cancel()

	// Init control structures.
	done := make(chan struct{})
	finishedCh := make(chan struct{})

	// Start workers.
	for i := 0; i < workers; i++ {
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
	for i := 0; i < b.N; i++ {
		<-finishedCh
	}
	b.StopTimer()

	// Cleanup.
	close(done)
}
