package terminal

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/structures/container"
)

const (
	timeoutTicks = 5

	clientTerminalAbandonTimeout = 15 * time.Second
	serverTerminalAbandonTimeout = 5 * time.Minute
)

// Terminal represents a terminal.
type Terminal interface { //nolint:golint // Being explicit is helpful here.
	// ID returns the terminal ID.
	ID() uint32
	// Ctx returns the terminal context.
	Ctx() context.Context

	// Deliver delivers a message to the terminal.
	// Should not be overridden by implementations.
	Deliver(msg *Msg) *Error
	// Send is used by others to send a message through the terminal.
	// Should not be overridden by implementations.
	Send(msg *Msg, timeout time.Duration) *Error
	// Flush sends all messages waiting in the terminal.
	// Should not be overridden by implementations.
	Flush(timeout time.Duration)

	// StartOperation starts the given operation by assigning it an ID and sending the given operation initialization data.
	// Should not be overridden by implementations.
	StartOperation(op Operation, initData *container.Container, timeout time.Duration) *Error
	// StopOperation stops the given operation.
	// Should not be overridden by implementations.
	StopOperation(op Operation, err *Error)

	// Abandon shuts down the terminal unregistering it from upstream and calling HandleAbandon().
	// Should not be overridden by implementations.
	Abandon(err *Error)
	// HandleAbandon gives the terminal the ability to cleanly shut down.
	// The terminal is still fully functional at this point.
	// The returned error is the error to send to the other side.
	// Should never be called directly. Call Abandon() instead.
	// Meant to be overridden by implementations.
	HandleAbandon(err *Error) (errorToSend *Error)
	// HandleDestruction gives the terminal the ability to clean up.
	// The terminal has already fully shut down at this point.
	// Should never be called directly. Call Abandon() instead.
	// Meant to be overridden by implementations.
	HandleDestruction(err *Error)

	// FmtID formats the terminal ID (including parent IDs).
	// May be overridden by implementations.
	FmtID() string
}

// TerminalBase contains the basic functions of a terminal.
type TerminalBase struct { //nolint:golint,maligned // Being explicit is helpful here.
	// TODO: Fix maligned.
	Terminal // Interface check.

	lock sync.RWMutex

	// id is the underlying id of the Terminal.
	id uint32
	// parentID is the id of the parent component.
	parentID string

	// ext holds the extended terminal so that the base terminal can access custom functions.
	ext Terminal
	// sendQueue holds message to be sent.
	sendQueue chan *Msg
	// flowControl holds the flow control system.
	flowControl FlowControl
	// upstream represents the upstream (parent) terminal.
	upstream Upstream

	// deliverProxy is populated with the configured deliver function
	deliverProxy func(msg *Msg) *Error
	// recvProxy is populated with the configured recv function
	recvProxy func() <-chan *Msg

	// ctx is the context of the Terminal.
	ctx context.Context
	// cancelCtx cancels ctx.
	cancelCtx context.CancelFunc

	// waitForFlush signifies if sending should be delayed until the next call
	// to Flush()
	waitForFlush *abool.AtomicBool
	// flush is used to send a finish function to the handler, which will write
	// all pending messages and then call the received function.
	flush chan func()
	// idleTicker ticks for increasing and checking the idle counter.
	idleTicker *time.Ticker
	// idleCounter counts the ticks the terminal has been idle.
	idleCounter *uint32

	// jession is the jess session used for encryption.
	jession *jess.Session
	// jessionLock locks jession.
	jessionLock sync.Mutex
	// encryptionReady is set when the encryption is ready for sending messages.
	encryptionReady chan struct{}
	// identity is the identity used by a remote Terminal.
	identity *cabin.Identity

	// operations holds references to all active operations that require persistence.
	operations map[uint32]Operation
	// nextOpID holds the next operation ID.
	nextOpID *uint32
	// permission holds the permissions of the terminal.
	permission Permission

	// opts holds the terminal options. It must not be modified after the terminal
	// has started.
	opts *TerminalOpts

	// lastUnknownOpID holds the operation ID of the last data message received
	// for an unknown operation ID.
	lastUnknownOpID uint32
	// lastUnknownOpMsgs holds the amount of continuous data messages received
	// for the operation ID in lastUnknownOpID.
	lastUnknownOpMsgs uint32

	// Abandoning indicates if the Terminal is being abandoned. The main handlers
	// will keep running until the context has been canceled by the abandon
	// procedure.
	// No new operations should be started.
	// Whoever initiates the abandoning must also start the abandon procedure.
	Abandoning *abool.AtomicBool
}

func createTerminalBase(
	ctx context.Context,
	id uint32,
	parentID string,
	remote bool,
	initMsg *TerminalOpts,
	upstream Upstream,
) (*TerminalBase, *Error) {
	t := &TerminalBase{
		id:              id,
		parentID:        parentID,
		sendQueue:       make(chan *Msg),
		upstream:        upstream,
		waitForFlush:    abool.New(),
		flush:           make(chan func()),
		idleTicker:      time.NewTicker(time.Minute),
		idleCounter:     new(uint32),
		encryptionReady: make(chan struct{}),
		operations:      make(map[uint32]Operation),
		nextOpID:        new(uint32),
		opts:            initMsg,
		Abandoning:      abool.New(),
	}
	// Stop ticking to disable timeout.
	t.idleTicker.Stop()
	// Shift next operation ID if remote.
	if remote {
		atomic.AddUint32(t.nextOpID, 4)
	}
	// Create context.
	t.ctx, t.cancelCtx = context.WithCancel(ctx)

	// Create flow control.
	switch initMsg.FlowControl {
	case FlowControlDFQ:
		t.flowControl = NewDuplexFlowQueue(t.Ctx(), initMsg.FlowControlSize, t.submitToUpstream)
		t.deliverProxy = t.flowControl.Deliver
		t.recvProxy = t.flowControl.Receive
	case FlowControlNone:
		deliver := make(chan *Msg, initMsg.FlowControlSize)
		t.deliverProxy = MakeDirectDeliveryDeliverFunc(ctx, deliver)
		t.recvProxy = MakeDirectDeliveryRecvFunc(deliver)
	case FlowControlDefault:
		fallthrough
	default:
		return nil, ErrInternalError.With("unknown flow control type %d", initMsg.FlowControl)
	}

	return t, nil
}

// ID returns the Terminal's ID.
func (t *TerminalBase) ID() uint32 {
	return t.id
}

// Ctx returns the Terminal's context.
func (t *TerminalBase) Ctx() context.Context {
	return t.ctx
}

// SetTerminalExtension sets the Terminal's extension. This function is not
// guarded and may only be used during initialization.
func (t *TerminalBase) SetTerminalExtension(ext Terminal) {
	t.ext = ext
}

// SetTimeout sets the Terminal's idle timeout duration.
// It is broken down into slots internally.
func (t *TerminalBase) SetTimeout(d time.Duration) {
	t.idleTicker.Reset(d / timeoutTicks)
}

// Deliver on TerminalBase only exists to conform to the interface. It must be
// overridden by an actual implementation.
func (t *TerminalBase) Deliver(msg *Msg) *Error {
	// Deliver via configured proxy.
	err := t.deliverProxy(msg)
	if err != nil {
		msg.Finish()
	}

	return err
}

// StartWorkers starts the necessary workers to operate the Terminal.
func (t *TerminalBase) StartWorkers(m *mgr.Manager, terminalName string) {
	// Start terminal workers.
	m.Go(terminalName+" handler", t.Handler)
	m.Go(terminalName+" sender", t.Sender)

	// Start any flow control workers.
	if t.flowControl != nil {
		t.flowControl.StartWorkers(m, terminalName)
	}
}

const (
	sendThresholdLength  = 100  // bytes
	sendMaxLength        = 4000 // bytes
	sendThresholdMaxWait = 20 * time.Millisecond
)

// Handler receives and handles messages and must be started as a worker in the
// module where the Terminal is used.
func (t *TerminalBase) Handler(_ *mgr.WorkerCtx) error {
	defer t.Abandon(ErrInternalError.With("handler died"))

	var msg *Msg
	defer msg.Finish()

	for {
		select {
		case <-t.ctx.Done():
			// Call Abandon just in case.
			// Normally, only the StopProcedure function should cancel the context.
			t.Abandon(nil)
			return nil // Controlled worker exit.

		case <-t.idleTicker.C:
			// If nothing happens for a while, end the session.
			if atomic.AddUint32(t.idleCounter, 1) > timeoutTicks {
				// Abandon the terminal and reset the counter.
				t.Abandon(ErrNoActivity)
				atomic.StoreUint32(t.idleCounter, 0)
			}

		case msg = <-t.recvProxy():
			err := t.handleReceive(msg)
			if err != nil {
				t.Abandon(err.Wrap("failed to handle"))
				return nil
			}

			// Register activity.
			atomic.StoreUint32(t.idleCounter, 0)
		}
	}
}

// submit is used to send message from the terminal to upstream, including
// going through flow control, if configured.
// This function should be used to send message from the terminal to upstream.
func (t *TerminalBase) submit(msg *Msg, timeout time.Duration) {
	// Submit directly if no flow control is configured.
	if t.flowControl == nil {
		t.submitToUpstream(msg, timeout)
		return
	}

	// Hand over to flow control.
	err := t.flowControl.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		t.Abandon(err.Wrap("failed to submit to flow control"))
	}
}

// submitToUpstream is used to directly submit messages to upstream.
// This function should only be used by the flow control or submit function.
func (t *TerminalBase) submitToUpstream(msg *Msg, timeout time.Duration) {
	// Add terminal ID as flow ID.
	msg.FlowID = t.ID()

	// Debug unit leaks.
	msg.debugWithCaller(2)

	// Submit to upstream.
	err := t.upstream.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		t.Abandon(err.Wrap("failed to submit to upstream"))
	}
}

// Sender handles sending messages and must be started as a worker in the
// module where the Terminal is used.
func (t *TerminalBase) Sender(_ *mgr.WorkerCtx) error {
	// Don't send messages, if the encryption is net yet set up.
	// The server encryption session is only initialized with the first
	// operative message, not on Terminal creation.
	if t.opts.Encrypt {
		select {
		case <-t.ctx.Done():
			// Call Abandon just in case.
			// Normally, the only the StopProcedure function should cancel the context.
			t.Abandon(nil)
			return nil // Controlled worker exit.
		case <-t.encryptionReady:
		}
	}

	// Be sure to call Stop even in case of sudden death.
	defer t.Abandon(ErrInternalError.With("sender died"))

	var msgBufferMsg *Msg
	var msgBufferLen int
	var msgBufferLimitReached bool
	var sendMsgs bool
	var sendMaxWait *time.Timer
	var flushFinished func()

	// Finish any current unit when returning.
	defer msgBufferMsg.Finish()

	// Only receive message when not sending the current msg buffer.
	sendQueueOpMsgs := func() <-chan *Msg {
		// Don't handle more messages, if the buffer is full.
		if msgBufferLimitReached {
			return nil
		}
		return t.sendQueue
	}

	// Only wait for sending slot when the current msg buffer is ready to be sent.
	readyToSend := func() <-chan struct{} {
		switch {
		case !sendMsgs:
			// Wait until there is something to send.
			return nil
		case t.flowControl != nil:
			// Let flow control decide when we are ready.
			return t.flowControl.ReadyToSend()
		default:
			// Always ready.
			return ready
		}
	}

	// Calculate current max wait time to send the msg buffer.
	getSendMaxWait := func() <-chan time.Time {
		if sendMaxWait != nil {
			return sendMaxWait.C
		}
		return nil
	}

handling:
	for {
		select {
		case <-t.ctx.Done():
			// Call Stop just in case.
			// Normally, the only the StopProcedure function should cancel the context.
			t.Abandon(nil)
			return nil // Controlled worker exit.

		case <-t.idleTicker.C:
			// If nothing happens for a while, end the session.
			if atomic.AddUint32(t.idleCounter, 1) > timeoutTicks {
				// Abandon the terminal and reset the counter.
				t.Abandon(ErrNoActivity)
				atomic.StoreUint32(t.idleCounter, 0)
			}

		case msg := <-sendQueueOpMsgs():
			if msg == nil {
				continue handling
			}

			// Add unit to buffer unit, or use it as new buffer.
			if msgBufferMsg != nil {
				// Pack, append and finish additional message.
				msgBufferMsg.Consume(msg)
			} else {
				// Pack operation message.
				msg.Pack()
				// Convert to message of terminal.
				msgBufferMsg = msg
				msgBufferMsg.FlowID = t.ID()
				msgBufferMsg.Type = MsgTypeData
			}
			msgBufferLen += msg.Data.Length()

			// Check if there is enough data to hit the sending threshold.
			if msgBufferLen >= sendThresholdLength {
				sendMsgs = true
			} else if sendMaxWait == nil && t.waitForFlush.IsNotSet() {
				sendMaxWait = time.NewTimer(sendThresholdMaxWait)
			}

			// Check if we have reached the maximum buffer size.
			if msgBufferLen >= sendMaxLength {
				msgBufferLimitReached = true
			}

			// Register activity.
			atomic.StoreUint32(t.idleCounter, 0)

		case <-getSendMaxWait():
			// The timer for waiting for more data has ended.
			// Send all available data if not forced to wait for a flush.
			if t.waitForFlush.IsNotSet() {
				sendMsgs = true
			}

		case newFlushFinishedFn := <-t.flush:
			// We are flushing - stop waiting.
			t.waitForFlush.UnSet()

			// Signal immediately if msg buffer is empty.
			if msgBufferLen == 0 {
				newFlushFinishedFn()
			} else {
				// If there already is a flush finished function, stack them.
				if flushFinished != nil {
					stackedFlushFinishFn := flushFinished
					flushFinished = func() {
						stackedFlushFinishFn()
						newFlushFinishedFn()
					}
				} else {
					flushFinished = newFlushFinishedFn
				}
			}

			// Force sending data now.
			sendMsgs = true

		case <-readyToSend():
			// Reset sending flags.
			sendMsgs = false
			msgBufferLimitReached = false

			// Send if there is anything to send.
			var err *Error
			if msgBufferLen > 0 {
				// Update message type to include priority.
				if msgBufferMsg.Type == MsgTypeData &&
					msgBufferMsg.Unit.IsHighPriority() &&
					t.opts.UsePriorityDataMsgs {
					msgBufferMsg.Type = MsgTypePriorityData
				}

				// Wait for clearance on initial msg only.
				msgBufferMsg.Unit.WaitForSlot()

				err = t.sendOpMsgs(msgBufferMsg)
			}

			// Reset buffer.
			msgBufferMsg = nil
			msgBufferLen = 0

			// Reset send wait timer.
			if sendMaxWait != nil {
				sendMaxWait.Stop()
				sendMaxWait = nil
			}

			// Check if we are flushing and need to notify.
			if flushFinished != nil {
				flushFinished()
				flushFinished = nil
			}

			// Handle error after state updates.
			if err != nil {
				t.Abandon(err.With("failed to send"))
				continue handling
			}
		}
	}
}

// WaitForFlush makes the terminal pause all sending until the next call to
// Flush().
func (t *TerminalBase) WaitForFlush() {
	t.waitForFlush.Set()
}

// Flush sends all data waiting to be sent.
func (t *TerminalBase) Flush(timeout time.Duration) {
	// Create channel and function for notifying.
	wait := make(chan struct{})
	finished := func() {
		close(wait)
	}
	// Request flush and return when stopping.
	select {
	case t.flush <- finished:
	case <-t.Ctx().Done():
		return
	case <-TimedOut(timeout):
		return
	}
	// Wait for flush to finish and return when stopping.
	select {
	case <-wait:
	case <-t.Ctx().Done():
		return
	case <-TimedOut(timeout):
		return
	}

	// Flush flow control, if configured.
	if t.flowControl != nil {
		t.flowControl.Flush(timeout)
	}
}

func (t *TerminalBase) encrypt(c *container.Container) (*container.Container, *Error) {
	if !t.opts.Encrypt {
		return c, nil
	}

	t.jessionLock.Lock()
	defer t.jessionLock.Unlock()

	letter, err := t.jession.Close(c.CompileData())
	if err != nil {
		return nil, ErrIntegrity.With("failed to encrypt: %w", err)
	}

	encryptedData, err := letter.ToWire()
	if err != nil {
		return nil, ErrInternalError.With("failed to pack letter: %w", err)
	}

	return encryptedData, nil
}

func (t *TerminalBase) decrypt(c *container.Container) (*container.Container, *Error) {
	if !t.opts.Encrypt {
		return c, nil
	}

	t.jessionLock.Lock()
	defer t.jessionLock.Unlock()

	letter, err := jess.LetterFromWire(c)
	if err != nil {
		return nil, ErrMalformedData.With("failed to parse letter: %w", err)
	}

	// Setup encryption if not yet done.
	if t.jession == nil {
		if t.identity == nil {
			return nil, ErrInternalError.With("missing identity for setting up incoming encryption")
		}

		// Create jess session.
		t.jession, err = letter.WireCorrespondence(t.identity)
		if err != nil {
			return nil, ErrIntegrity.With("failed to initialize incoming encryption: %w", err)
		}

		// Don't need that anymore.
		t.identity = nil

		// Encryption is ready for sending.
		close(t.encryptionReady)
	}

	decryptedData, err := t.jession.Open(letter)
	if err != nil {
		return nil, ErrIntegrity.With("failed to decrypt: %w", err)
	}

	return container.New(decryptedData), nil
}

func (t *TerminalBase) handleReceive(msg *Msg) *Error {
	msg.Unit.WaitForSlot()
	defer msg.Finish()

	// Debugging:
	// log.Errorf("spn/terminal %s handling tmsg: %s", t.FmtID(), spew.Sdump(c.CompileData()))

	// Check if message is empty. This will be the case if a message was only
	// for updated the available space of the flow queue.
	if !msg.Data.HoldsData() {
		return nil
	}

	// Decrypt if enabled.
	var tErr *Error
	msg.Data, tErr = t.decrypt(msg.Data)
	if tErr != nil {
		return tErr
	}

	// Handle operation messages.
	for msg.Data.HoldsData() {
		// Get next message length.
		msgLength, err := msg.Data.GetNextN32()
		if err != nil {
			return ErrMalformedData.With("failed to get operation msg length: %w", err)
		}
		if msgLength == 0 {
			// Remainder is padding.
			// Padding can only be at the end of the segment.
			t.handlePaddingMsg(msg.Data)
			return nil
		}

		// Get op msg data.
		msgData, err := msg.Data.GetAsContainer(int(msgLength))
		if err != nil {
			return ErrMalformedData.With("failed to get operation msg data (%d/%d bytes): %w", msg.Data.Length(), msgLength, err)
		}

		// Handle op msg.
		if handleErr := t.handleOpMsg(msgData); handleErr != nil {
			return handleErr
		}
	}

	return nil
}

func (t *TerminalBase) handleOpMsg(data *container.Container) *Error {
	// Debugging:
	// log.Errorf("spn/terminal %s handling opmsg: %s", t.FmtID(), spew.Sdump(data.CompileData()))

	// Parse message operation id, type.
	opID, msgType, err := ParseIDType(data)
	if err != nil {
		return ErrMalformedData.With("failed to parse operation msg id/type: %w", err)
	}

	switch msgType {
	case MsgTypeInit:
		t.handleOperationStart(opID, data)

	case MsgTypeData, MsgTypePriorityData:
		op, ok := t.GetActiveOp(opID)
		if ok && !op.Stopped() {
			// Create message from data.
			msg := NewEmptyMsg()
			msg.FlowID = opID
			msg.Type = msgType
			msg.Data = data
			if msg.Type == MsgTypePriorityData {
				msg.Unit.MakeHighPriority()
			}

			// Deliver message to operation.
			tErr := op.Deliver(msg)
			if tErr != nil {
				// Also stop on "success" errors!
				msg.Finish()
				t.StopOperation(op, tErr)
			}
			return nil
		}

		// If an active op is not found, this is likely just left-overs from a
		// stopped or failed operation.
		// log.Tracef("spn/terminal: %s received data msg for unknown op %d", fmtTerminalID(t.parentID, t.id), opID)

		// Send a stop error if this happens too often.
		if opID == t.lastUnknownOpID {
			// OpID is the same as last time.
			t.lastUnknownOpMsgs++

			// Log an warning (via StopOperation) and send a stop message every thousand.
			if t.lastUnknownOpMsgs%1000 == 0 {
				t.StopOperation(newUnknownOp(opID, ""), ErrUnknownOperationID.With("received %d unsolicited data msgs", t.lastUnknownOpMsgs))
			}

			// TODO: Abandon terminal at over 10000?
		} else {
			// OpID changed, set new ID and reset counter.
			t.lastUnknownOpID = opID
			t.lastUnknownOpMsgs = 1
		}

	case MsgTypeStop:
		// Parse received error.
		opErr, parseErr := ParseExternalError(data.CompileData())
		if parseErr != nil {
			log.Warningf("spn/terminal: %s failed to parse stop error: %s", fmtTerminalID(t.parentID, t.id), parseErr)
			opErr = ErrUnknownError.AsExternal()
		}

		// End operation.
		op, ok := t.GetActiveOp(opID)
		if ok {
			t.StopOperation(op, opErr)
		} else {
			log.Tracef("spn/terminal: %s received stop msg for unknown op %d", fmtTerminalID(t.parentID, t.id), opID)
		}

	default:
		log.Warningf("spn/terminal: %s received unexpected message type: %d", t.FmtID(), msgType)
		return ErrUnexpectedMsgType
	}

	return nil
}

func (t *TerminalBase) handlePaddingMsg(c *container.Container) {
	padding := c.GetAll()
	if len(padding) > 0 {
		rngFeeder.SupplyEntropyIfNeeded(padding, len(padding))
	}
}

func (t *TerminalBase) sendOpMsgs(msg *Msg) *Error {
	msg.Unit.WaitForSlot()

	// Add Padding if needed.
	if t.opts.Padding > 0 {
		paddingNeeded := (int(t.opts.Padding) - msg.Data.Length()) % int(t.opts.Padding)
		if paddingNeeded > 0 {
			// Add padding message header.
			msg.Data.Append([]byte{0})
			paddingNeeded--

			// Add needed padding data.
			if paddingNeeded > 0 {
				padding, err := rng.Bytes(paddingNeeded)
				if err != nil {
					log.Debugf("spn/terminal: %s failed to get random data, using zeros instead", t.FmtID())
					padding = make([]byte, paddingNeeded)
				}
				msg.Data.Append(padding)
			}
		}
	}

	// Encrypt operative data.
	var tErr *Error
	msg.Data, tErr = t.encrypt(msg.Data)
	if tErr != nil {
		return tErr
	}

	// Send data.
	t.submit(msg, 0)
	return nil
}

// Abandon shuts down the terminal unregistering it from upstream and calling HandleAbandon().
// Should not be overridden by implementations.
func (t *TerminalBase) Abandon(err *Error) {
	if t.Abandoning.SetToIf(false, true) {
		module.mgr.Go("terminal abandon procedure", func(_ *mgr.WorkerCtx) error {
			t.handleAbandonProcedure(err)
			return nil
		})
	}
}

// HandleAbandon gives the terminal the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Abandon() instead.
// Meant to be overridden by implementations.
func (t *TerminalBase) HandleAbandon(err *Error) (errorToSend *Error) {
	return err
}

// HandleDestruction gives the terminal the ability to clean up.
// The terminal has already fully shut down at this point.
// Should never be called directly. Call Abandon() instead.
// Meant to be overridden by implementations.
func (t *TerminalBase) HandleDestruction(err *Error) {}

func (t *TerminalBase) handleAbandonProcedure(err *Error) {
	// End all operations.
	for _, op := range t.allOps() {
		t.StopOperation(op, nil)
	}

	// Prepare timeouts for waiting for ops.
	timeout := clientTerminalAbandonTimeout
	if conf.PublicHub() {
		timeout = serverTerminalAbandonTimeout
	}
	checkTicker := time.NewTicker(50 * time.Millisecond)
	defer checkTicker.Stop()
	abortWaiting := time.After(timeout)

	// Wait for all operations to end.
waitForOps:
	for {
		select {
		case <-checkTicker.C:
			if t.GetActiveOpCount() <= 0 {
				break waitForOps
			}
		case <-abortWaiting:
			log.Warningf(
				"spn/terminal: terminal %s is continuing shutdown with %d active operations",
				t.FmtID(),
				t.GetActiveOpCount(),
			)
			break waitForOps
		}
	}

	// Call operation stop handle function for proper shutdown cleaning up.
	if t.ext != nil {
		err = t.ext.HandleAbandon(err)
	}

	// Send error to the connected Operation, if the error is internal.
	if !err.IsExternal() {
		if err == nil {
			err = ErrStopping
		}

		msg := NewMsg(err.Pack())
		msg.FlowID = t.ID()
		msg.Type = MsgTypeStop
		t.submit(msg, 1*time.Second)
	}

	// If terminal was ended locally, send all data before abandoning.
	// If terminal was ended remotely, don't bother sending remaining data.
	if !err.IsExternal() {
		// Flushing could mean sending a full buffer of 50000 packets.
		t.Flush(5 * time.Minute)
	}

	// Stop all other connected workers.
	t.cancelCtx()
	t.idleTicker.Stop()

	// Call operation destruction handle function for proper shutdown cleaning up.
	if t.ext != nil {
		t.ext.HandleDestruction(err)
	}
}

func (t *TerminalBase) allOps() []Operation {
	t.lock.Lock()
	defer t.lock.Unlock()

	ops := make([]Operation, 0, len(t.operations))
	for _, op := range t.operations {
		ops = append(ops, op)
	}

	return ops
}

// MakeDirectDeliveryDeliverFunc creates a submit upstream function with the
// given delivery channel.
func MakeDirectDeliveryDeliverFunc(
	ctx context.Context,
	deliver chan *Msg,
) func(c *Msg) *Error {
	return func(c *Msg) *Error {
		select {
		case deliver <- c:
			return nil
		case <-ctx.Done():
			return ErrStopping
		}
	}
}

// MakeDirectDeliveryRecvFunc makes a delivery receive function with the given
// delivery channel.
func MakeDirectDeliveryRecvFunc(
	deliver chan *Msg,
) func() <-chan *Msg {
	return func() <-chan *Msg {
		return deliver
	}
}
