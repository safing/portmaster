package terminal

import (
	"context"
	"testing"
	"time"
)

type startFailTestOp struct {
	OperationBase

	stopped chan *Error
}

func (op *startFailTestOp) Type() string {
	return "test/startfail"
}

func (op *startFailTestOp) HandleStop(err *Error) (errorToSend *Error) {
	select {
	case op.stopped <- err:
	default:
	}
	return err
}

// TestStartOperationFailureCleansUp checks that an operation whose init msg
// could not be sent does not stay registered (leaked) in the terminal and is
// given the chance to clean up.
func TestStartOperationFailureCleansUp(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(module.mgr.Ctx())
	defer cancel()

	// Create a terminal without starting its workers. This simulates a
	// terminal whose sender is busy, eg. waiting for flow control space, so
	// that sending the init msg times out.
	term, _, tErr := NewLocalBaseTerminal(
		ctx, 127, "t", nil,
		&TerminalOpts{
			FlowControl:     FlowControlDFQ,
			FlowControlSize: defaultTestQueueSize,
		},
		UpstreamSendFunc(func(msg *Msg, _ time.Duration) *Error {
			msg.Finish()
			return nil
		}),
	)
	if tErr != nil {
		t.Fatalf("failed to create terminal: %s", tErr)
	}

	op := &startFailTestOp{stopped: make(chan *Error, 1)}
	tErr = term.StartOperation(op, nil, 10*time.Millisecond)
	if tErr == nil {
		t.Fatal("expected starting the operation to fail")
	}

	// The failed operation must not be registered anymore.
	if cnt := term.GetActiveOpCount(); cnt != 0 {
		t.Fatalf("failed operation is still registered: %d active operations", cnt)
	}

	// The operation must be stopped, so it can clean up.
	select {
	case <-op.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("failed operation was not stopped")
	}
	if !op.Stopped() {
		t.Fatal("failed operation is not marked as stopped")
	}
}
