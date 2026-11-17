package terminal

import (
	"sync"
	"testing"
	"time"

	"github.com/safing/structures/container"
)

// TestRngFeederRestart checks that the entropy feeder can be replaced on a
// module restart while padding messages are being handled concurrently.
// Run with -race.
func TestRngFeederRestart(t *testing.T) { //nolint:paralleltest // Restarts the global feeder.
	term := &TerminalBase{}
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
			term.handlePaddingMsg(container.New([]byte("padding")))
		}
	})

	// Restart the module like a module stop and start does.
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
}
