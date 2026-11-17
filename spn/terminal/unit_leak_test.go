package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/unit"
)

// useStatScheduler replaces the global scheduler with one that cycles its
// stats quickly and returns a function that reports whether any unit of that
// scheduler was finished within the observation window.
// The unit lifetime stat is only non-zero if at least one unit was finished
// in the last cycle, which makes leaked units observable from the outside.
func useStatScheduler(t *testing.T) (anyUnitFinished func(wait time.Duration) bool) {
	t.Helper()

	old := getScheduler()
	s := unit.NewScheduler(&unit.SchedulerConfig{
		SlotDuration:      time.Millisecond,
		StatCycleDuration: 20 * time.Millisecond,
	})
	scheduler.Store(s)
	ctx, cancel := context.WithCancel(context.Background())
	module.mgr.Go("test msg unit scheduler", s.SlotScheduler)
	t.Cleanup(func() {
		cancel()
		s.Stop()
		scheduler.Store(old)
	})

	return func(wait time.Duration) bool {
		deadline := time.Now().Add(wait)
		for time.Now().Before(deadline) {
			if s.GetAvgUnitLife() > 0 {
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Millisecond):
			}
		}
		return false
	}
}

// TestSenderFinishesBufferedUnit checks that the unit of a message that is
// still buffered when the sender exits is finished.
func TestSenderFinishesBufferedUnit(t *testing.T) { //nolint:paralleltest // Replaces the global scheduler.
	anyUnitFinished := useStatScheduler(t)

	term, _, tErr := NewLocalBaseTerminal(
		context.Background(), 1, "test", nil, &TerminalOpts{Padding: 0},
		UpstreamSendFunc(func(msg *Msg, _ time.Duration) *Error {
			msg.Finish()
			return nil
		}),
	)
	if tErr != nil {
		t.Fatal(tErr)
	}

	// Never flush on timeout, so that the message stays in the buffer.
	term.WaitForFlush()

	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		_ = term.Sender(nil) //nolint:errcheck
	}()

	msg := NewMsg([]byte("buffered"))
	select {
	case term.sendQueue <- msg:
	case <-time.After(time.Second):
		t.Fatal("sender did not accept msg")
	}

	// Keep the message buffered for a moment: the stat only counts units with
	// a lifetime above zero, and on Windows the monotonic clock only advances
	// every few milliseconds.
	time.Sleep(20 * time.Millisecond)

	// Stop the sender while the message is buffered.
	term.cancelCtx()
	select {
	case <-senderDone:
	case <-time.After(time.Second):
		t.Fatal("sender did not exit")
	}

	if !anyUnitFinished(500 * time.Millisecond) {
		t.Fatal("unit of the buffered msg was not finished when the sender exited")
	}
}
