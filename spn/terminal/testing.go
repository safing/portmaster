package terminal

import (
	"context"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/structures/container"
)

const (
	defaultTestQueueSize = 16
	defaultTestPadding   = 8
	logTestCraneMsgs     = false
)

// TestTerminal is a terminal for running tests.
type TestTerminal struct {
	*TerminalBase
}

// NewLocalTestTerminal returns a new local test terminal.
func NewLocalTestTerminal(
	ctx context.Context,
	id uint32,
	parentID string,
	remoteHub *hub.Hub,
	initMsg *TerminalOpts,
	upstream Upstream,
) (*TestTerminal, *container.Container, *Error) {
	// Create Terminal Base.
	t, initData, err := NewLocalBaseTerminal(ctx, id, parentID, remoteHub, initMsg, upstream)
	if err != nil {
		return nil, nil, err
	}
	t.StartWorkers(module.mgr, "test terminal")

	return &TestTerminal{t}, initData, nil
}

// NewRemoteTestTerminal returns a new remote test terminal.
func NewRemoteTestTerminal(
	ctx context.Context,
	id uint32,
	parentID string,
	identity *cabin.Identity,
	initData *container.Container,
	upstream Upstream,
) (*TestTerminal, *TerminalOpts, *Error) {
	// Create Terminal Base.
	t, initMsg, err := NewRemoteBaseTerminal(ctx, id, parentID, identity, initData, upstream)
	if err != nil {
		return nil, nil, err
	}
	t.StartWorkers(module.mgr, "test terminal")

	return &TestTerminal{t}, initMsg, nil
}

type delayedMsg struct {
	msg        *Msg
	timeout    time.Duration
	delayUntil time.Time
}

func createDelayingTestForwardingFunc(
	srcName,
	dstName string,
	delay time.Duration,
	delayQueueSize int,
	deliverFunc func(msg *Msg, timeout time.Duration) *Error,
) func(msg *Msg, timeout time.Duration) *Error {
	// Return simple forward func if no delay is given.
	if delay == 0 {
		return func(msg *Msg, timeout time.Duration) *Error {
			// Deliver to other terminal.
			dErr := deliverFunc(msg, timeout)
			if dErr != nil {
				log.Errorf("spn/testing: %s>%s: failed to deliver to terminal: %s", srcName, dstName, dErr)
				return dErr
			}
			return nil
		}
	}

	// If there is delay, create a delaying channel and handler.
	delayedMsgs := make(chan *delayedMsg, delayQueueSize)
	go func() {
		for {
			// Read from chan
			msg := <-delayedMsgs
			if msg == nil {
				return
			}

			// Check if we need to wait.
			waitFor := time.Until(msg.delayUntil)
			if waitFor > 0 {
				time.Sleep(waitFor)
			}

			// Deliver to other terminal.
			dErr := deliverFunc(msg.msg, msg.timeout)
			if dErr != nil {
				log.Errorf("spn/testing: %s>%s: failed to deliver to terminal: %s", srcName, dstName, dErr)
			}
		}
	}()

	return func(msg *Msg, timeout time.Duration) *Error {
		// Add msg to delaying msg channel.
		delayedMsgs <- &delayedMsg{
			msg:        msg,
			timeout:    timeout,
			delayUntil: time.Now().Add(delay),
		}
		return nil
	}
}

// HandleAbandon gives the terminal the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Abandon() instead.
func (t *TestTerminal) HandleAbandon(err *Error) (errorToSend *Error) {
	switch err {
	case nil:
		// nil means that the Terminal is being shutdown by the owner.
		log.Tracef("spn/terminal: %s is closing", fmtTerminalID(t.parentID, t.id))
	default:
		// All other errors are faults.
		log.Warningf("spn/terminal: %s: %s", fmtTerminalID(t.parentID, t.id), err)
	}

	return
}

// NewSimpleTestTerminalPair provides a simple connected terminal pair for tests.
func NewSimpleTestTerminalPair(delay time.Duration, delayQueueSize int, opts *TerminalOpts) (a, b *TestTerminal, err error) {
	if opts == nil {
		opts = &TerminalOpts{
			Padding:         defaultTestPadding,
			FlowControl:     FlowControlDFQ,
			FlowControlSize: defaultTestQueueSize,
		}
	}

	var initData *container.Container
	var tErr *Error
	a, initData, tErr = NewLocalTestTerminal(
		module.mgr.Ctx(), 127, "a", nil, opts, UpstreamSendFunc(createDelayingTestForwardingFunc(
			"a", "b", delay, delayQueueSize, func(msg *Msg, timeout time.Duration) *Error {
				return b.Deliver(msg)
			},
		)),
	)
	if tErr != nil {
		return nil, nil, tErr.Wrap("failed to create local test terminal")
	}
	b, _, tErr = NewRemoteTestTerminal(
		module.mgr.Ctx(), 127, "b", nil, initData, UpstreamSendFunc(createDelayingTestForwardingFunc(
			"b", "a", delay, delayQueueSize, func(msg *Msg, timeout time.Duration) *Error {
				return a.Deliver(msg)
			},
		)),
	)
	if tErr != nil {
		return nil, nil, tErr.Wrap("failed to create remote test terminal")
	}

	return a, b, nil
}

// BareTerminal is a bare terminal that just returns errors for testing.
type BareTerminal struct{}

var (
	_ Terminal = &BareTerminal{}

	errNotImplementedByBareTerminal = ErrInternalError.With("not implemented by bare terminal")
)

// ID returns the terminal ID.
func (t *BareTerminal) ID() uint32 {
	return 0
}

// Ctx returns the terminal context.
func (t *BareTerminal) Ctx() context.Context {
	return context.Background()
}

// Deliver delivers a message to the terminal.
// Should not be overridden by implementations.
func (t *BareTerminal) Deliver(msg *Msg) *Error {
	return errNotImplementedByBareTerminal
}

// Send is used by others to send a message through the terminal.
// Should not be overridden by implementations.
func (t *BareTerminal) Send(msg *Msg, timeout time.Duration) *Error {
	return errNotImplementedByBareTerminal
}

// Flush sends all messages waiting in the terminal.
// Should not be overridden by implementations.
func (t *BareTerminal) Flush(timeout time.Duration) {}

// StartOperation starts the given operation by assigning it an ID and sending the given operation initialization data.
// Should not be overridden by implementations.
func (t *BareTerminal) StartOperation(op Operation, initData *container.Container, timeout time.Duration) *Error {
	return errNotImplementedByBareTerminal
}

// StopOperation stops the given operation.
// Should not be overridden by implementations.
func (t *BareTerminal) StopOperation(op Operation, err *Error) {}

// Abandon shuts down the terminal unregistering it from upstream and calling HandleAbandon().
// Should not be overridden by implementations.
func (t *BareTerminal) Abandon(err *Error) {}

// HandleAbandon gives the terminal the ability to cleanly shut down.
// The terminal is still fully functional at this point.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Abandon() instead.
// Meant to be overridden by implementations.
func (t *BareTerminal) HandleAbandon(err *Error) (errorToSend *Error) {
	return err
}

// HandleDestruction gives the terminal the ability to clean up.
// The terminal has already fully shut down at this point.
// Should never be called directly. Call Abandon() instead.
// Meant to be overridden by implementations.
func (t *BareTerminal) HandleDestruction(err *Error) {}

// FmtID formats the terminal ID (including parent IDs).
// May be overridden by implementations.
func (t *BareTerminal) FmtID() string {
	return "bare"
}
