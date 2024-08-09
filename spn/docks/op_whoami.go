package docks

import (
	"time"

	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

const (
	// WhoAmIType is the type ID of the latency test operation.
	WhoAmIType = "whoami"

	whoAmITimeout = 3 * time.Second
)

// WhoAmIOp is used to request some metadata about the other side.
type WhoAmIOp struct {
	terminal.OneOffOperationBase

	response *WhoAmIResponse
}

// WhoAmIResponse is a whoami response.
type WhoAmIResponse struct {
	// Timestamp in nanoseconds
	Timestamp int64 `cbor:"t,omitempty" json:"t,omitempty"`

	// Addr is the remote address as reported by the crane terminal (IP and port).
	Addr string `cbor:"a,omitempty" json:"a,omitempty"`
}

// Type returns the type ID.
func (op *WhoAmIOp) Type() string {
	return WhoAmIType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:  WhoAmIType,
		Start: startWhoAmI,
	})
}

// WhoAmI executes a whoami operation and returns the response.
func WhoAmI(t terminal.Terminal) (*WhoAmIResponse, *terminal.Error) {
	whoami, err := NewWhoAmIOp(t)
	if err.IsError() {
		return nil, err
	}

	// Wait for response.
	select {
	case tErr := <-whoami.Result:
		if tErr.IsError() {
			return nil, tErr
		}
		return whoami.response, nil
	case <-time.After(whoAmITimeout * 2):
		return nil, terminal.ErrTimeout
	}
}

// NewWhoAmIOp starts a new whoami operation.
func NewWhoAmIOp(t terminal.Terminal) (*WhoAmIOp, *terminal.Error) {
	// Create operation and init.
	op := &WhoAmIOp{}
	op.OneOffOperationBase.Init()

	// Send ping.
	tErr := t.StartOperation(op, nil, whoAmITimeout)
	if tErr != nil {
		return nil, tErr
	}

	return op, nil
}

// Deliver delivers a message to the operation.
func (op *WhoAmIOp) Deliver(msg *terminal.Msg) *terminal.Error {
	defer msg.Finish()

	// Parse response.
	response := &WhoAmIResponse{}
	_, err := dsd.Load(msg.Data.CompileData(), response)
	if err != nil {
		return terminal.ErrMalformedData.With("failed to parse ping response: %w", err)
	}

	op.response = response
	return terminal.ErrExplicitAck
}

func startWhoAmI(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Get crane terminal, if available.
	ct, _ := t.(*CraneTerminal)

	// Create response.
	r := &WhoAmIResponse{
		Timestamp: time.Now().UnixNano(),
	}
	if ct != nil {
		r.Addr = ct.RemoteAddr().String()
	}
	response, err := dsd.Dump(r, dsd.CBOR)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to create whoami response: %w", err)
	}

	// Send response.
	msg := terminal.NewMsg(response)
	msg.FlowID = opID
	msg.Unit.MakeHighPriority()
	if terminal.UsePriorityDataMsgs {
		msg.Type = terminal.MsgTypePriorityData
	}
	tErr := t.Send(msg, whoAmITimeout)
	if tErr != nil {
		// Finish message unit on failure.
		msg.Finish()
		return nil, tErr.With("failed to send ping response")
	}

	// Operation is just one response and finished successfully.
	return nil, nil
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *WhoAmIOp) HandleStop(err *terminal.Error) (errorToSend *terminal.Error) {
	// Continue with usual handling of inherited base.
	return op.OneOffOperationBase.HandleStop(err)
}
