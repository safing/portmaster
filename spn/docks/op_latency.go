package docks

import (
	"bytes"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

const (
	// LatencyTestOpType is the type ID of the latency test operation.
	LatencyTestOpType = "latency"

	latencyPingRequest  = 1
	latencyPingResponse = 2

	latencyTestNonceSize = 16
	latencyTestRuns      = 10
)

var (
	latencyTestPauseDuration = 1 * time.Second
	latencyTestOpTimeout     = latencyTestRuns * latencyTestPauseDuration * 3
)

// LatencyTestOp is used to measure latency.
type LatencyTestOp struct {
	terminal.OperationBase
}

// LatencyTestClientOp is the client version of LatencyTestOp.
type LatencyTestClientOp struct {
	LatencyTestOp

	lastPingSentAt    time.Time
	lastPingNonce     []byte
	measuredLatencies []time.Duration
	responses         chan *terminal.Msg
	testResult        time.Duration

	result chan *terminal.Error
}

// Type returns the type ID.
func (op *LatencyTestOp) Type() string {
	return LatencyTestOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     LatencyTestOpType,
		Requires: terminal.IsCraneController,
		Start:    startLatencyTestOp,
	})
}

// NewLatencyTestOp runs a latency test.
func NewLatencyTestOp(t terminal.Terminal) (*LatencyTestClientOp, *terminal.Error) {
	// Create and init.
	op := &LatencyTestClientOp{
		responses:         make(chan *terminal.Msg),
		measuredLatencies: make([]time.Duration, 0, latencyTestRuns),
		result:            make(chan *terminal.Error, 1),
	}

	// Make ping request.
	pingRequest, err := op.createPingRequest()
	if err != nil {
		return nil, terminal.ErrInternalError.With("%w", err)
	}

	// Send ping.
	tErr := t.StartOperation(op, pingRequest, 1*time.Second)
	if tErr != nil {
		return nil, tErr
	}

	// Start handler.
	module.mgr.Go("op latency handler", op.handler)

	return op, nil
}

func (op *LatencyTestClientOp) handler(ctx *mgr.WorkerCtx) error {
	returnErr := terminal.ErrStopping
	defer func() {
		// Linters don't get that returnErr is used when directly used as defer.
		op.Stop(op, returnErr)
	}()

	var nextTest <-chan time.Time
	opTimeout := time.After(latencyTestOpTimeout)

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-opTimeout:
			return nil

		case <-nextTest:
			// Create ping request msg.
			pingRequest, err := op.createPingRequest()
			if err != nil {
				returnErr = terminal.ErrInternalError.With("%w", err)
				return nil
			}
			msg := op.NewEmptyMsg()
			msg.Unit.MakeHighPriority()
			msg.Data = pingRequest

			// Send it.
			tErr := op.Send(msg, latencyTestOpTimeout)
			if tErr != nil {
				returnErr = tErr.Wrap("failed to send ping request")
				return nil
			}
			op.Flush(1 * time.Second)

			nextTest = nil

		case msg := <-op.responses:
			// Check if the op ended.
			if msg == nil {
				return nil
			}

			// Handle response
			tErr := op.handleResponse(msg)
			if tErr != nil {
				returnErr = tErr
				return nil //nolint:nilerr
			}

			// Check if we have enough latency tests.
			if len(op.measuredLatencies) >= latencyTestRuns {
				returnErr = op.reportMeasuredLatencies()
				return nil
			}

			// Schedule next latency test, if not yet scheduled.
			if nextTest == nil {
				nextTest = time.After(latencyTestPauseDuration)
			}
		}
	}
}

func (op *LatencyTestClientOp) createPingRequest() (*container.Container, error) {
	// Generate nonce.
	nonce, err := rng.Bytes(latencyTestNonceSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create ping nonce")
	}

	// Set client request state.
	op.lastPingSentAt = time.Now()
	op.lastPingNonce = nonce

	return container.New(
		varint.Pack8(latencyPingRequest),
		nonce,
	), nil
}

func (op *LatencyTestClientOp) handleResponse(msg *terminal.Msg) *terminal.Error {
	defer msg.Finish()

	rType, err := msg.Data.GetNextN8()
	if err != nil {
		return terminal.ErrMalformedData.With("failed to get response type: %w", err)
	}

	switch rType {
	case latencyPingResponse:
		// Check if the ping nonce matches.
		if !bytes.Equal(op.lastPingNonce, msg.Data.CompileData()) {
			return terminal.ErrIntegrity.With("ping nonce mismatch")
		}
		op.lastPingNonce = nil
		// Save latency.
		op.measuredLatencies = append(op.measuredLatencies, time.Since(op.lastPingSentAt))

		return nil
	default:
		return terminal.ErrIncorrectUsage.With("unknown response type")
	}
}

func (op *LatencyTestClientOp) reportMeasuredLatencies() *terminal.Error {
	// Find lowest value.
	lowestLatency := time.Hour
	for _, latency := range op.measuredLatencies {
		if latency < lowestLatency {
			lowestLatency = latency
		}
	}
	op.testResult = lowestLatency

	// Save the result to the crane.
	if controller, ok := op.Terminal().(*CraneControllerTerminal); ok {
		if controller.Crane.ConnectedHub != nil {
			controller.Crane.ConnectedHub.GetMeasurements().SetLatency(op.testResult)
			log.Infof("spn/docks: measured latency to %s: %s", controller.Crane.ConnectedHub, op.testResult)
			return nil
		} else if controller.Crane.IsMine() {
			return terminal.ErrInternalError.With("latency operation was run on %s without a connected hub set", controller.Crane)
		}
	} else if !runningTests {
		return terminal.ErrInternalError.With("latency operation was run on terminal that is not a crane controller, but %T", op.Terminal())
	}
	return nil
}

// Deliver delivers a message to the operation.
func (op *LatencyTestClientOp) Deliver(msg *terminal.Msg) *terminal.Error {
	// Optimized delivery with 1s timeout.
	select {
	case op.responses <- msg:
	default:
		select {
		case op.responses <- msg:
		case <-time.After(1 * time.Second):
			return terminal.ErrTimeout
		}
	}
	return nil
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *LatencyTestClientOp) HandleStop(tErr *terminal.Error) (errorToSend *terminal.Error) {
	close(op.responses)
	select {
	case op.result <- tErr:
	default:
	}
	return tErr
}

// Result returns the result (end error) of the operation.
func (op *LatencyTestClientOp) Result() <-chan *terminal.Error {
	return op.result
}

func startLatencyTestOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Create operation.
	op := &LatencyTestOp{}
	op.InitOperationBase(t, opID)

	// Handle first request.
	msg := op.NewEmptyMsg()
	msg.Data = data
	tErr := op.Deliver(msg)
	if tErr != nil {
		return nil, tErr
	}

	return op, nil
}

// Deliver delivers a message to the operation.
func (op *LatencyTestOp) Deliver(msg *terminal.Msg) *terminal.Error {
	// Get request type.
	rType, err := msg.Data.GetNextN8()
	if err != nil {
		return terminal.ErrMalformedData.With("failed to get response type: %w", err)
	}

	switch rType {
	case latencyPingRequest:
		// Keep the nonce and just replace the msg type.
		msg.Data.PrependNumber(latencyPingResponse)
		msg.Type = terminal.MsgTypeData
		msg.Unit.ReUse()
		msg.Unit.MakeHighPriority()

		// Send response.
		tErr := op.Send(msg, latencyTestOpTimeout)
		if tErr != nil {
			return tErr.Wrap("failed to send ping response")
		}
		op.Flush(1 * time.Second)

		return nil

	default:
		return terminal.ErrIncorrectUsage.With("unknown request type")
	}
}
