package terminal

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/structures/container"
)

// Operation is an interface for all operations.
type Operation interface {
	// InitOperationBase initialize the operation with the ID and attached terminal.
	// Should not be overridden by implementations.
	InitOperationBase(t Terminal, opID uint32)

	// ID returns the ID of the operation.
	// Should not be overridden by implementations.
	ID() uint32

	// Type returns the operation's type ID.
	// Should be overridden by implementations to return correct type ID.
	Type() string

	// Deliver delivers a message to the operation.
	// Meant to be overridden by implementations.
	Deliver(msg *Msg) *Error

	// NewMsg creates a new message from this operation.
	// Should not be overridden by implementations.
	NewMsg(data []byte) *Msg

	// Send sends a message to the other side.
	// Should not be overridden by implementations.
	Send(msg *Msg, timeout time.Duration) *Error

	// Flush sends all messages waiting in the terminal.
	// Should not be overridden by implementations.
	Flush(timeout time.Duration)

	// Stopped returns whether the operation has stopped.
	// Should not be overridden by implementations.
	Stopped() bool

	// markStopped marks the operation as stopped.
	// It returns whether the stop flag was set.
	markStopped() bool

	// Stop stops the operation by unregistering it from the terminal and calling HandleStop().
	// Should not be overridden by implementations.
	Stop(self Operation, err *Error)

	// HandleStop gives the operation the ability to cleanly shut down.
	// The returned error is the error to send to the other side.
	// Should never be called directly. Call Stop() instead.
	// Meant to be overridden by implementations.
	HandleStop(err *Error) (errorToSend *Error)

	// Terminal returns the terminal the operation is linked to.
	// Should not be overridden by implementations.
	Terminal() Terminal
}

// OperationFactory defines an operation factory.
type OperationFactory struct {
	// Type is the type id of an operation.
	Type string
	// Requires defines the required permissions to run an operation.
	Requires Permission
	// Start is the function that starts a new operation.
	Start OperationStarter
}

// OperationStarter is used to initialize operations remotely.
type OperationStarter func(attachedTerminal Terminal, opID uint32, initData *container.Container) (Operation, *Error)

var (
	opRegistry       = make(map[string]*OperationFactory)
	opRegistryLock   sync.Mutex
	opRegistryLocked = abool.New()
)

// RegisterOpType registers a new operation type and may only be called during
// Go's init and a module's prep phase.
func RegisterOpType(factory OperationFactory) {
	// Check if we can still register an operation type.
	if opRegistryLocked.IsSet() {
		log.Errorf("spn/terminal: failed to register operation %s: operation registry is already locked", factory.Type)
		return
	}

	opRegistryLock.Lock()
	defer opRegistryLock.Unlock()

	// Check if the operation type was already registered.
	if _, ok := opRegistry[factory.Type]; ok {
		log.Errorf("spn/terminal: failed to register operation type %s: type already registered", factory.Type)
		return
	}

	// Save to registry.
	opRegistry[factory.Type] = &factory
}

func lockOpRegistry() {
	opRegistryLocked.Set()
}

func (t *TerminalBase) handleOperationStart(opID uint32, initData *container.Container) {
	// Check if the terminal is being abandoned.
	if t.Abandoning.IsSet() {
		t.StopOperation(newUnknownOp(opID, ""), ErrAbandonedTerminal)
		return
	}

	// Extract the requested operation name.
	opType, err := initData.GetNextBlock()
	if err != nil {
		t.StopOperation(newUnknownOp(opID, ""), ErrMalformedData.With("failed to get init data: %w", err))
		return
	}

	// Get the operation factory from the registry.
	factory, ok := opRegistry[string(opType)]
	if !ok {
		t.StopOperation(newUnknownOp(opID, ""), ErrUnknownOperationType.With(utils.SafeFirst16Bytes(opType)))
		return
	}

	// Check if the Terminal has the required permission to run the operation.
	if !t.HasPermission(factory.Requires) {
		t.StopOperation(newUnknownOp(opID, factory.Type), ErrPermissionDenied)
		return
	}

	// Get terminal to attach to.
	attachToTerminal := t.ext
	if attachToTerminal == nil {
		attachToTerminal = t
	}

	// Run the operation.
	op, opErr := factory.Start(attachToTerminal, opID, initData)
	switch {
	case opErr != nil:
		// Something went wrong.
		t.StopOperation(newUnknownOp(opID, factory.Type), opErr)
	case op == nil:
		// The Operation was successful and is done already.
		log.Debugf("spn/terminal: operation %s %s executed", factory.Type, fmtOperationID(t.parentID, t.id, opID))
		t.StopOperation(newUnknownOp(opID, factory.Type), nil)
	default:
		// The operation started successfully and requires persistence.
		t.SetActiveOp(opID, op)
		log.Debugf("spn/terminal: operation %s %s started", factory.Type, fmtOperationID(t.parentID, t.id, opID))
	}
}

// StartOperation starts the given operation by assigning it an ID and sending the given operation initialization data.
func (t *TerminalBase) StartOperation(op Operation, initData *container.Container, timeout time.Duration) *Error {
	// Get terminal to attach to.
	attachToTerminal := t.ext
	if attachToTerminal == nil {
		attachToTerminal = t
	}

	// Get the next operation ID and set it on the operation with the terminal.
	op.InitOperationBase(attachToTerminal, atomic.AddUint32(t.nextOpID, 8))

	// Always add operation to the active operations, as we need to receive a
	// reply in any case.
	t.SetActiveOp(op.ID(), op)

	log.Debugf("spn/terminal: operation %s %s started", op.Type(), fmtOperationID(t.parentID, t.id, op.ID()))

	// Add or create the operation type block.
	if initData == nil {
		initData = container.New()
		initData.AppendAsBlock([]byte(op.Type()))
	} else {
		initData.PrependAsBlock([]byte(op.Type()))
	}

	// Create init msg.
	msg := NewEmptyMsg()
	msg.FlowID = op.ID()
	msg.Type = MsgTypeInit
	msg.Data = initData
	msg.Unit.MakeHighPriority()

	// Send init msg.
	err := op.Send(msg, timeout)
	if err != nil {
		msg.Finish()
	}
	return err
}

// Send sends data via this terminal.
// If a timeout is set, sending will fail after the given timeout passed.
func (t *TerminalBase) Send(msg *Msg, timeout time.Duration) *Error {
	// Wait for processing slot.
	msg.Unit.WaitForSlot()

	// Check if the send queue has available space.
	select {
	case t.sendQueue <- msg:
		return nil
	default:
	}

	// Submit message to buffer, if space is available.
	select {
	case t.sendQueue <- msg:
		return nil
	case <-TimedOut(timeout):
		msg.Finish()
		return ErrTimeout.With("sending via terminal")
	case <-t.Ctx().Done():
		msg.Finish()
		return ErrStopping
	}
}

// StopOperation sends the end signal with an optional error and then deletes
// the operation from the Terminal state and calls HandleStop() on the Operation.
func (t *TerminalBase) StopOperation(op Operation, err *Error) {
	// Check if the operation has already stopped.
	if !op.markStopped() {
		return
	}

	// Log reason the Operation is ending. Override stopping error with nil.
	switch {
	case err == nil:
		log.Debugf("spn/terminal: operation %s %s stopped", op.Type(), fmtOperationID(t.parentID, t.id, op.ID()))
	case err.IsOK(), err.Is(ErrTryAgainLater), err.Is(ErrRateLimited):
		log.Debugf("spn/terminal: operation %s %s stopped: %s", op.Type(), fmtOperationID(t.parentID, t.id, op.ID()), err)
	default:
		log.Warningf("spn/terminal: operation %s %s failed: %s", op.Type(), fmtOperationID(t.parentID, t.id, op.ID()), err)
	}

	module.mgr.Go("stop operation", func(_ *mgr.WorkerCtx) error {
		// Call operation stop handle function for proper shutdown cleaning up.
		err = op.HandleStop(err)

		// Send error to the connected Operation, if the error is internal.
		if !err.IsExternal() {
			if err == nil {
				err = ErrStopping
			}

			msg := NewMsg(err.Pack())
			msg.FlowID = op.ID()
			msg.Type = MsgTypeStop

			tErr := t.Send(msg, 10*time.Second)
			if tErr != nil {
				msg.Finish()
				log.Warningf("spn/terminal: failed to send stop msg: %s", tErr)
			}
		}

		// Remove operation from terminal.
		t.DeleteActiveOp(op.ID())

		return nil
	})
}

// GetActiveOp returns the active operation with the given ID from the
// Terminal state.
func (t *TerminalBase) GetActiveOp(opID uint32) (op Operation, ok bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	op, ok = t.operations[opID]
	return
}

// SetActiveOp saves an active operation to the Terminal state.
func (t *TerminalBase) SetActiveOp(opID uint32, op Operation) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.operations[opID] = op
}

// DeleteActiveOp deletes an active operation from the Terminal state.
func (t *TerminalBase) DeleteActiveOp(opID uint32) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.operations, opID)
}

// GetActiveOpCount returns the amount of active operations.
func (t *TerminalBase) GetActiveOpCount() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.operations)
}

func newUnknownOp(id uint32, typeID string) *unknownOp {
	op := &unknownOp{
		typeID: typeID,
	}
	op.id = id
	return op
}

type unknownOp struct {
	OperationBase
	typeID string
}

func (op *unknownOp) Type() string {
	if op.typeID != "" {
		return op.typeID
	}
	return "unknown"
}

func (op *unknownOp) Deliver(msg *Msg) *Error {
	return ErrIncorrectUsage.With("unknown op shim cannot receive")
}
