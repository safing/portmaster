package terminal

import (
	"sync"
	"testing"
	"time"
)

// TestSchedulerRestart checks that the unit scheduler can be replaced on a
// module restart while messages are being created concurrently.
// Run with -race.
func TestSchedulerRestart(t *testing.T) { //nolint:paralleltest // Restarts the global scheduler.
	stop := make(chan struct{})
	running := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		close(running)
		for {
			select {
			case <-stop:
				return
			default:
			}
			NewMsg([]byte("test")).Finish()
			NewEmptyMsg().Finish()
		}
	})

	// Restart the scheduler like a module stop and start does.
	<-running
	for range 3 {
		time.Sleep(10 * time.Millisecond)
		StopScheduler()
		err := start()
		if err != nil {
			t.Fatalf("failed to restart: %s", err)
		}
	}

	close(stop)
	wg.Wait()

	// Messages must still get through the new scheduler.
	msg := NewMsg([]byte("test"))
	msg.Unit.WaitForSlot()
	msg.Finish()
}
