package docks

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
)

// ExpandOpType is the type ID of the expand operation.
const ExpandOpType string = "expand"

var activeExpandOps = new(int64)

// ExpandOp is used to expand to another Hub.
type ExpandOp struct {
	terminal.OperationBase
	opts *terminal.TerminalOpts

	// ctx is the context of the Terminal.
	ctx context.Context
	// cancelCtx cancels ctx.
	cancelCtx context.CancelFunc

	dataRelayed *uint64
	ended       *abool.AtomicBool

	relayTerminal *ExpansionRelayTerminal

	// flowControl holds the flow control system.
	flowControl terminal.FlowControl
	// deliverProxy is populated with the configured deliver function
	deliverProxy func(msg *terminal.Msg) *terminal.Error
	// recvProxy is populated with the configured recv function
	recvProxy func() <-chan *terminal.Msg
	// sendProxy is populated with the configured send function
	sendProxy func(msg *terminal.Msg, timeout time.Duration)
}

// ExpansionRelayTerminal is a relay used for expansion.
type ExpansionRelayTerminal struct {
	terminal.BareTerminal

	op *ExpandOp

	id    uint32
	crane *Crane

	abandoning *abool.AtomicBool

	// flowControl holds the flow control system.
	flowControl terminal.FlowControl
	// deliverProxy is populated with the configured deliver function
	deliverProxy func(msg *terminal.Msg) *terminal.Error
	// recvProxy is populated with the configured recv function
	recvProxy func() <-chan *terminal.Msg
	// sendProxy is populated with the configured send function
	sendProxy func(msg *terminal.Msg, timeout time.Duration)
}

// Type returns the type ID.
func (op *ExpandOp) Type() string {
	return ExpandOpType
}

// ID returns the operation ID.
func (t *ExpansionRelayTerminal) ID() uint32 {
	return t.id
}

// Ctx returns the operation context.
func (op *ExpandOp) Ctx() context.Context {
	return op.ctx
}

// Ctx returns the relay terminal context.
func (t *ExpansionRelayTerminal) Ctx() context.Context {
	return t.op.ctx
}

// Deliver delivers a message to the relay operation.
func (op *ExpandOp) Deliver(msg *terminal.Msg) *terminal.Error {
	return op.deliverProxy(msg)
}

// Deliver delivers a message to the relay terminal.
func (t *ExpansionRelayTerminal) Deliver(msg *terminal.Msg) *terminal.Error {
	return t.deliverProxy(msg)
}

// Flush writes all data in the queues.
func (op *ExpandOp) Flush(timeout time.Duration) {
	if op.flowControl != nil {
		op.flowControl.Flush(timeout)
	}
}

// Flush writes all data in the queues.
func (t *ExpansionRelayTerminal) Flush(timeout time.Duration) {
	if t.flowControl != nil {
		t.flowControl.Flush(timeout)
	}
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     ExpandOpType,
		Requires: terminal.MayExpand,
		Start:    expand,
	})
}

func expand(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Submit metrics.
	newExpandOp.Inc()

	// Check if we are running a public hub.
	if !conf.PublicHub() {
		return nil, terminal.ErrPermissionDenied.With("expanding is only allowed on public hubs")
	}

	// Parse destination hub ID.
	dstData, err := data.GetNextBlock()
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to parse destination: %w", err)
	}

	// Parse terminal options.
	opts, tErr := terminal.ParseTerminalOpts(data)
	if tErr != nil {
		return nil, tErr.Wrap("failed to parse terminal options")
	}

	// Get crane with destination.
	relayCrane := GetAssignedCrane(string(dstData))
	if relayCrane == nil {
		return nil, terminal.ErrHubUnavailable.With("no crane assigned to %q", string(dstData))
	}

	// TODO: Expand outside of hot path.

	// Create operation and terminal.
	op := &ExpandOp{
		opts:        opts,
		dataRelayed: new(uint64),
		ended:       abool.New(),
		relayTerminal: &ExpansionRelayTerminal{
			crane:      relayCrane,
			id:         relayCrane.getNextTerminalID(),
			abandoning: abool.New(),
		},
	}
	op.InitOperationBase(t, opID)
	op.ctx, op.cancelCtx = context.WithCancel(t.Ctx())
	op.relayTerminal.op = op

	// Create flow control.
	switch opts.FlowControl {
	case terminal.FlowControlDFQ:
		// Operation
		op.flowControl = terminal.NewDuplexFlowQueue(op.ctx, opts.FlowControlSize, op.submitBackwardUpstream)
		op.deliverProxy = op.flowControl.Deliver
		op.recvProxy = op.flowControl.Receive
		op.sendProxy = op.submitBackwardFlowControl
		// Relay Terminal
		op.relayTerminal.flowControl = terminal.NewDuplexFlowQueue(op.ctx, opts.FlowControlSize, op.submitForwardUpstream)
		op.relayTerminal.deliverProxy = op.relayTerminal.flowControl.Deliver
		op.relayTerminal.recvProxy = op.relayTerminal.flowControl.Receive
		op.relayTerminal.sendProxy = op.submitForwardFlowControl
	case terminal.FlowControlNone:
		// Operation
		deliverToOp := make(chan *terminal.Msg, opts.FlowControlSize)
		op.deliverProxy = terminal.MakeDirectDeliveryDeliverFunc(op.ctx, deliverToOp)
		op.recvProxy = terminal.MakeDirectDeliveryRecvFunc(deliverToOp)
		op.sendProxy = op.submitBackwardUpstream
		// Relay Terminal
		deliverToRelay := make(chan *terminal.Msg, opts.FlowControlSize)
		op.relayTerminal.deliverProxy = terminal.MakeDirectDeliveryDeliverFunc(op.ctx, deliverToRelay)
		op.relayTerminal.recvProxy = terminal.MakeDirectDeliveryRecvFunc(deliverToRelay)
		op.relayTerminal.sendProxy = op.submitForwardUpstream
	case terminal.FlowControlDefault:
		fallthrough
	default:
		return nil, terminal.ErrInternalError.With("unknown flow control type %d", opts.FlowControl)
	}

	// Establish terminal on destination.
	newInitData, tErr := opts.Pack()
	if tErr != nil {
		return nil, terminal.ErrInternalError.With("failed to re-pack options: %w", err)
	}
	tErr = op.relayTerminal.crane.EstablishNewTerminal(op.relayTerminal, newInitData)
	if tErr != nil {
		return nil, tErr
	}

	// Start workers.
	module.mgr.Go("expand op forward relay", op.forwardHandler)
	module.mgr.Go("expand op backward relay", op.backwardHandler)
	if op.flowControl != nil {
		op.flowControl.StartWorkers(module.mgr, "expand op")
	}
	if op.relayTerminal.flowControl != nil {
		op.relayTerminal.flowControl.StartWorkers(module.mgr, "expand op terminal")
	}

	return op, nil
}

func (op *ExpandOp) submitForwardFlowControl(msg *terminal.Msg, timeout time.Duration) {
	err := op.relayTerminal.flowControl.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		op.Stop(op, err.Wrap("failed to submit to forward flow control"))
	}
}

func (op *ExpandOp) submitBackwardFlowControl(msg *terminal.Msg, timeout time.Duration) {
	err := op.flowControl.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		op.Stop(op, err.Wrap("failed to submit to backward flow control"))
	}
}

func (op *ExpandOp) submitForwardUpstream(msg *terminal.Msg, timeout time.Duration) {
	msg.FlowID = op.relayTerminal.id
	if msg.Unit.IsHighPriority() && op.opts.UsePriorityDataMsgs {
		msg.Type = terminal.MsgTypePriorityData
	} else {
		msg.Type = terminal.MsgTypeData
	}
	err := op.relayTerminal.crane.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		op.Stop(op, err.Wrap("failed to submit to forward upstream"))
	}
}

func (op *ExpandOp) submitBackwardUpstream(msg *terminal.Msg, timeout time.Duration) {
	msg.FlowID = op.relayTerminal.id
	if msg.Unit.IsHighPriority() && op.opts.UsePriorityDataMsgs {
		msg.Type = terminal.MsgTypePriorityData
	} else {
		msg.Type = terminal.MsgTypeData
		msg.Unit.RemovePriority()
	}
	// Note: op.Send() will transform high priority units to priority data msgs.
	err := op.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		op.Stop(op, err.Wrap("failed to submit to backward upstream"))
	}
}

func (op *ExpandOp) forwardHandler(_ *mgr.WorkerCtx) error {
	// Metrics setup and submitting.
	atomic.AddInt64(activeExpandOps, 1)
	started := time.Now()
	defer func() {
		atomic.AddInt64(activeExpandOps, -1)
		expandOpDurationHistogram.UpdateDuration(started)
		expandOpRelayedDataHistogram.Update(float64(atomic.LoadUint64(op.dataRelayed)))
	}()

	for {
		select {
		case msg := <-op.recvProxy():
			// Debugging:
			// log.Debugf("spn/testing: forwarding at %s: %s", op.FmtID(), spew.Sdump(c.CompileData()))

			// Wait for processing slot.
			msg.Unit.WaitForSlot()

			// Count relayed data for metrics.
			atomic.AddUint64(op.dataRelayed, uint64(msg.Data.Length()))

			// Receive data from the origin and forward it to the relay.
			op.relayTerminal.sendProxy(msg, 1*time.Minute)

		case <-op.ctx.Done():
			return nil
		}
	}
}

func (op *ExpandOp) backwardHandler(_ *mgr.WorkerCtx) error {
	for {
		select {
		case msg := <-op.relayTerminal.recvProxy():
			// Debugging:
			// log.Debugf("spn/testing: backwarding at %s: %s", op.FmtID(), spew.Sdump(c.CompileData()))

			// Wait for processing slot.
			msg.Unit.WaitForSlot()

			// Count relayed data for metrics.
			atomic.AddUint64(op.dataRelayed, uint64(msg.Data.Length()))

			// Receive data from the relay and forward it to the origin.
			op.sendProxy(msg, 1*time.Minute)

		case <-op.ctx.Done():
			return nil
		}
	}
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *ExpandOp) HandleStop(err *terminal.Error) (errorToSend *terminal.Error) {
	// Flush all messages before stopping.
	op.Flush(1 * time.Minute)
	op.relayTerminal.Flush(1 * time.Minute)

	// Stop connected workers.
	op.cancelCtx()

	// Abandon connected terminal.
	op.relayTerminal.Abandon(nil)

	// Add context to error.
	if err.IsError() {
		return err.Wrap("relay operation failed with")
	}
	return err
}

// Abandon shuts down the terminal unregistering it from upstream and calling HandleAbandon().
func (t *ExpansionRelayTerminal) Abandon(err *terminal.Error) {
	if t.abandoning.SetToIf(false, true) {
		module.mgr.Go("terminal abandon procedure", func(_ *mgr.WorkerCtx) error {
			t.handleAbandonProcedure(err)
			return nil
		})
	}
}

// HandleAbandon gives the terminal the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Abandon() instead.
func (t *ExpansionRelayTerminal) HandleAbandon(err *terminal.Error) (errorToSend *terminal.Error) {
	// Stop the connected relay operation.
	t.op.Stop(t.op, err)

	// Add context to error.
	if err.IsError() {
		return err.Wrap("relay terminal failed with")
	}
	return err
}

// HandleDestruction gives the terminal the ability to clean up.
// The terminal has already fully shut down at this point.
// Should never be called directly. Call Abandon() instead.
func (t *ExpansionRelayTerminal) HandleDestruction(err *terminal.Error) {}

func (t *ExpansionRelayTerminal) handleAbandonProcedure(err *terminal.Error) {
	// Call operation stop handle function for proper shutdown cleaning up.
	err = t.HandleAbandon(err)

	// Flush all messages before stopping.
	t.Flush(1 * time.Minute)

	// Send error to the connected Operation, if the error is internal.
	if !err.IsExternal() {
		if err == nil {
			err = terminal.ErrStopping
		}

		msg := terminal.NewMsg(err.Pack())
		msg.FlowID = t.ID()
		msg.Type = terminal.MsgTypeStop
		t.op.submitForwardUpstream(msg, 1*time.Second)
	}
}

// FmtID returns the expansion ID hierarchy.
func (op *ExpandOp) FmtID() string {
	return fmt.Sprintf("%s>%d <r> %s#%d", op.Terminal().FmtID(), op.ID(), op.relayTerminal.crane.ID, op.relayTerminal.id)
}

// FmtID returns the expansion ID hierarchy.
func (t *ExpansionRelayTerminal) FmtID() string {
	return fmt.Sprintf("%s#%d", t.crane.ID, t.id)
}
