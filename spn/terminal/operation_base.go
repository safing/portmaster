package terminal

import (
	"time"

	"github.com/tevino/abool"
)

// OperationBase provides the basic operation functionality.
type OperationBase struct {
	terminal Terminal
	id       uint32
	stopped  abool.AtomicBool
}

// InitOperationBase initialize the operation with the ID and attached terminal.
// Should not be overridden by implementations.
func (op *OperationBase) InitOperationBase(t Terminal, opID uint32) {
	op.id = opID
	op.terminal = t
}

// ID returns the ID of the operation.
// Should not be overridden by implementations.
func (op *OperationBase) ID() uint32 {
	return op.id
}

// Type returns the operation's type ID.
// Should be overridden by implementations to return correct type ID.
func (op *OperationBase) Type() string {
	return "unknown"
}

// Deliver delivers a message to the operation.
// Meant to be overridden by implementations.
func (op *OperationBase) Deliver(_ *Msg) *Error {
	return ErrIncorrectUsage.With("Deliver not implemented for this operation")
}

// NewMsg creates a new message from this operation.
// Should not be overridden by implementations.
func (op *OperationBase) NewMsg(data []byte) *Msg {
	msg := NewMsg(data)
	msg.FlowID = op.id
	msg.Type = MsgTypeData

	// Debug unit leaks.
	msg.debugWithCaller(2)

	return msg
}

// NewEmptyMsg creates a new empty message from this operation.
// Should not be overridden by implementations.
func (op *OperationBase) NewEmptyMsg() *Msg {
	msg := NewEmptyMsg()
	msg.FlowID = op.id
	msg.Type = MsgTypeData

	// Debug unit leaks.
	msg.debugWithCaller(2)

	return msg
}

// Send sends a message to the other side.
// Should not be overridden by implementations.
func (op *OperationBase) Send(msg *Msg, timeout time.Duration) *Error {
	// Add and update metadata.
	msg.FlowID = op.id
	if msg.Type == MsgTypeData && msg.Unit.IsHighPriority() && UsePriorityDataMsgs {
		msg.Type = MsgTypePriorityData
	}

	// Wait for processing slot.
	msg.Unit.WaitForSlot()

	// Send message.
	tErr := op.terminal.Send(msg, timeout)
	if tErr != nil {
		// Finish message unit on failure.
		msg.Finish()
	}
	return tErr
}

// Flush sends all messages waiting in the terminal.
// Meant to be overridden by implementations.
func (op *OperationBase) Flush(timeout time.Duration) {
	op.terminal.Flush(timeout)
}

// Stopped returns whether the operation has stopped.
// Should not be overridden by implementations.
func (op *OperationBase) Stopped() bool {
	return op.stopped.IsSet()
}

// markStopped marks the operation as stopped.
// It returns whether the stop flag was set.
func (op *OperationBase) markStopped() bool {
	return op.stopped.SetToIf(false, true)
}

// Stop stops the operation by unregistering it from the terminal and calling HandleStop().
// Should not be overridden by implementations.
func (op *OperationBase) Stop(self Operation, err *Error) {
	// Stop operation from terminal.
	op.terminal.StopOperation(self, err)
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
// Meant to be overridden by implementations.
func (op *OperationBase) HandleStop(err *Error) (errorToSend *Error) {
	return err
}

// Terminal returns the terminal the operation is linked to.
// Should not be overridden by implementations.
func (op *OperationBase) Terminal() Terminal {
	return op.terminal
}

// OneOffOperationBase is an operation base for operations that just have one
// message and a error return.
type OneOffOperationBase struct {
	OperationBase

	Result chan *Error
}

// Init initializes the single operation base.
func (op *OneOffOperationBase) Init() {
	op.Result = make(chan *Error, 1)
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *OneOffOperationBase) HandleStop(err *Error) (errorToSend *Error) {
	select {
	case op.Result <- err:
	default:
	}
	return err
}

// MessageStreamOperationBase is an operation base for receiving a message stream.
// Every received message must be finished by the implementing operation.
type MessageStreamOperationBase struct {
	OperationBase

	Delivered chan *Msg
	Ended     chan *Error
}

// Init initializes the operation base.
func (op *MessageStreamOperationBase) Init(deliverQueueSize int) {
	op.Delivered = make(chan *Msg, deliverQueueSize)
	op.Ended = make(chan *Error, 1)
}

// Deliver delivers data to the operation.
func (op *MessageStreamOperationBase) Deliver(msg *Msg) *Error {
	select {
	case op.Delivered <- msg:
		return nil
	default:
		return ErrIncorrectUsage.With("request was not waiting for data")
	}
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *MessageStreamOperationBase) HandleStop(err *Error) (errorToSend *Error) {
	select {
	case op.Ended <- err:
	default:
	}
	return err
}
