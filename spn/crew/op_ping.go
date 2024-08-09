package crew

import (
	"crypto/subtle"
	"time"

	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

const (
	// PingOpType is the type ID of the latency test operation.
	PingOpType = "ping"

	pingOpNonceSize = 16
	pingOpTimeout   = 3 * time.Second
)

// PingOp is used to measure latency.
type PingOp struct {
	terminal.OneOffOperationBase

	started time.Time
	nonce   []byte
}

// PingOpRequest is a ping request.
type PingOpRequest struct {
	Nonce []byte `json:"n,omitempty"`
}

// PingOpResponse is a ping response.
type PingOpResponse struct {
	Nonce []byte    `json:"n,omitempty"`
	Time  time.Time `json:"t,omitempty"`
}

// Type returns the type ID.
func (op *PingOp) Type() string {
	return PingOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:  PingOpType,
		Start: startPingOp,
	})
}

// NewPingOp runs a latency test.
func NewPingOp(t terminal.Terminal) (*PingOp, *terminal.Error) {
	// Generate nonce.
	nonce, err := rng.Bytes(pingOpNonceSize)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to generate ping nonce: %w", err)
	}

	// Create operation and init.
	op := &PingOp{
		started: time.Now().UTC(),
		nonce:   nonce,
	}
	op.OneOffOperationBase.Init()

	// Create request.
	pingRequest, err := dsd.Dump(&PingOpRequest{
		Nonce: op.nonce,
	}, dsd.CBOR)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to create ping request: %w", err)
	}

	// Send ping.
	tErr := t.StartOperation(op, container.New(pingRequest), pingOpTimeout)
	if tErr != nil {
		return nil, tErr
	}

	return op, nil
}

// Deliver delivers a message to the operation.
func (op *PingOp) Deliver(msg *terminal.Msg) *terminal.Error {
	defer msg.Finish()

	// Parse response.
	response := &PingOpResponse{}
	_, err := dsd.Load(msg.Data.CompileData(), response)
	if err != nil {
		return terminal.ErrMalformedData.With("failed to parse ping response: %w", err)
	}

	// Check if the nonce matches.
	if subtle.ConstantTimeCompare(op.nonce, response.Nonce) != 1 {
		return terminal.ErrIntegrity.With("ping nonce mismatched")
	}

	return terminal.ErrExplicitAck
}

func startPingOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Parse request.
	request := &PingOpRequest{}
	_, err := dsd.Load(data.CompileData(), request)
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to parse ping request: %w", err)
	}

	// Create response.
	response, err := dsd.Dump(&PingOpResponse{
		Nonce: request.Nonce,
		Time:  time.Now().UTC(),
	}, dsd.CBOR)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to create ping response: %w", err)
	}

	// Send response.
	msg := terminal.NewMsg(response)
	msg.FlowID = opID
	msg.Unit.MakeHighPriority()
	if terminal.UsePriorityDataMsgs {
		msg.Type = terminal.MsgTypePriorityData
	}
	tErr := t.Send(msg, pingOpTimeout)
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
func (op *PingOp) HandleStop(err *terminal.Error) (errorToSend *terminal.Error) {
	// Prevent remote from sending explicit ack, as we use it as a success signal internally.
	if err.Is(terminal.ErrExplicitAck) && err.IsExternal() {
		err = terminal.ErrStopping.AsExternal()
	}

	// Continue with usual handling of inherited base.
	return op.OneOffOperationBase.HandleStop(err)
}
