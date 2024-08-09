package docks

import (
	"bytes"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

const (
	// CapacityTestOpType is the type ID of the capacity test operation.
	CapacityTestOpType = "capacity"

	defaultCapacityTestVolume = 50000000  // 50MB
	maxCapacityTestVolume     = 100000000 // 100MB

	defaultCapacityTestMaxTime = 5 * time.Second
	maxCapacityTestMaxTime     = 15 * time.Second
	capacityTestTimeout        = 30 * time.Second

	capacityTestMsgSize     = 1000
	capacityTestSendTimeout = 1000 * time.Millisecond
)

var (
	capacityTestSendData           = make([]byte, capacityTestMsgSize)
	capacityTestDataReceivedSignal = []byte("ACK")

	capacityTestRunning = abool.New()
)

// CapacityTestOp is used for capacity test operations.
type CapacityTestOp struct { //nolint:maligned
	terminal.OperationBase

	opts *CapacityTestOptions

	started       bool
	startTime     time.Time
	senderStarted bool

	recvQueue              chan *terminal.Msg
	dataReceived           int
	dataReceivedAckWasAckd bool

	dataSent        *int64
	dataSentWasAckd *abool.AtomicBool

	testResult int
	result     chan *terminal.Error
}

// CapacityTestOptions holds options for the capacity test.
type CapacityTestOptions struct {
	TestVolume int
	MaxTime    time.Duration
	testing    bool
}

// Type returns the type ID.
func (op *CapacityTestOp) Type() string {
	return CapacityTestOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     CapacityTestOpType,
		Requires: terminal.IsCraneController,
		Start:    startCapacityTestOp,
	})
}

// NewCapacityTestOp runs a capacity test.
func NewCapacityTestOp(t terminal.Terminal, opts *CapacityTestOptions) (*CapacityTestOp, *terminal.Error) {
	// Check options.
	if opts == nil {
		opts = &CapacityTestOptions{
			TestVolume: defaultCapacityTestVolume,
			MaxTime:    defaultCapacityTestMaxTime,
		}
	}

	// Check if another test is already running.
	if !opts.testing && !capacityTestRunning.SetToIf(false, true) {
		return nil, terminal.ErrTryAgainLater.With("another capacity op is already running")
	}

	// Create and init.
	op := &CapacityTestOp{
		opts:            opts,
		recvQueue:       make(chan *terminal.Msg),
		dataSent:        new(int64),
		dataSentWasAckd: abool.New(),
		result:          make(chan *terminal.Error, 1),
	}

	// Make capacity test request.
	request, err := dsd.Dump(op.opts, dsd.CBOR)
	if err != nil {
		capacityTestRunning.UnSet()
		return nil, terminal.ErrInternalError.With("failed to serialize capactity test options: %w", err)
	}

	// Send test request.
	tErr := t.StartOperation(op, container.New(request), 1*time.Second)
	if tErr != nil {
		capacityTestRunning.UnSet()
		return nil, tErr
	}

	// Start handler.
	module.mgr.Go("op capacity handler", op.handler)

	return op, nil
}

func startCapacityTestOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Check if another test is already running.
	if !capacityTestRunning.SetToIf(false, true) {
		return nil, terminal.ErrTryAgainLater.With("another capacity op is already running")
	}

	// Parse options.
	opts := &CapacityTestOptions{}
	_, err := dsd.Load(data.CompileData(), opts)
	if err != nil {
		capacityTestRunning.UnSet()
		return nil, terminal.ErrMalformedData.With("failed to parse options: %w", err)
	}

	// Check options.
	if opts.TestVolume > maxCapacityTestVolume {
		capacityTestRunning.UnSet()
		return nil, terminal.ErrInvalidOptions.With("maximum volume exceeded")
	}
	if opts.MaxTime > maxCapacityTestMaxTime {
		capacityTestRunning.UnSet()
		return nil, terminal.ErrInvalidOptions.With("maximum maxtime exceeded")
	}

	// Create operation.
	op := &CapacityTestOp{
		opts:            opts,
		recvQueue:       make(chan *terminal.Msg, 1000),
		dataSent:        new(int64),
		dataSentWasAckd: abool.New(),
		result:          make(chan *terminal.Error, 1),
	}
	op.InitOperationBase(t, opID)

	// Start handler and sender.
	op.senderStarted = true
	module.mgr.Go("op capacity handler", op.handler)
	module.mgr.Go("op capacity sender", op.sender)

	return op, nil
}

func (op *CapacityTestOp) handler(ctx *mgr.WorkerCtx) error {
	defer capacityTestRunning.UnSet()

	returnErr := terminal.ErrStopping
	defer func() {
		// Linters don't get that returnErr is used when directly used as defer.
		op.Stop(op, returnErr)
	}()

	var maxTestTimeReached <-chan time.Time
	opTimeout := time.After(capacityTestTimeout)

	// Setup unit handling
	var msg *terminal.Msg
	defer msg.Finish()

	// Handle receives.
	for {
		msg.Finish()

		select {
		case <-ctx.Done():
			returnErr = terminal.ErrCanceled
			return nil

		case <-opTimeout:
			returnErr = terminal.ErrTimeout
			return nil

		case <-maxTestTimeReached:
			returnErr = op.reportMeasuredCapacity()
			return nil

		case msg = <-op.recvQueue:
			// Record start time and start sender.
			if !op.started {
				op.started = true
				op.startTime = time.Now()
				maxTestTimeReached = time.After(op.opts.MaxTime)
				if !op.senderStarted {
					op.senderStarted = true
					module.mgr.Go("op capacity sender", op.sender)
				}
			}

			// Add to received data counter.
			op.dataReceived += msg.Data.Length()

			// Check if we received the data received signal.
			if msg.Data.Length() == len(capacityTestDataReceivedSignal) &&
				bytes.Equal(msg.Data.CompileData(), capacityTestDataReceivedSignal) {
				op.dataSentWasAckd.Set()
			}

			// Send the data received signal when we received the full test volume.
			if op.dataReceived >= op.opts.TestVolume && !op.dataReceivedAckWasAckd {
				tErr := op.Send(op.NewMsg(capacityTestDataReceivedSignal), capacityTestSendTimeout)
				if tErr != nil {
					returnErr = tErr.Wrap("failed to send data received signal")
					return nil
				}
				atomic.AddInt64(op.dataSent, int64(len(capacityTestDataReceivedSignal)))
				op.dataReceivedAckWasAckd = true

				// Flush last message.
				op.Flush(10 * time.Second)
			}

			// Check if we can complete the test.
			if op.dataReceivedAckWasAckd &&
				op.dataSentWasAckd.IsSet() {
				returnErr = op.reportMeasuredCapacity()
				return nil
			}
		}
	}
}

func (op *CapacityTestOp) sender(ctx *mgr.WorkerCtx) error {
	for {
		// Send next chunk.
		msg := op.NewMsg(capacityTestSendData)
		msg.Unit.MakeHighPriority()
		tErr := op.Send(msg, capacityTestSendTimeout)
		if tErr != nil {
			op.Stop(op, tErr.Wrap("failed to send capacity test data"))
			return nil
		}

		// Add to sent data counter and stop sending if sending is complete.
		if atomic.AddInt64(op.dataSent, int64(len(capacityTestSendData))) >= int64(op.opts.TestVolume) {
			return nil
		}

		// Check if we have received an ack.
		if op.dataSentWasAckd.IsSet() {
			return nil
		}

		// Check if op has ended.
		if op.Stopped() {
			return nil
		}
	}
}

func (op *CapacityTestOp) reportMeasuredCapacity() *terminal.Error {
	// Calculate lane capacity and set it.
	timeNeeded := time.Since(op.startTime)
	if timeNeeded <= 0 {
		timeNeeded = 1
	}
	duplexBits := float64((int64(op.dataReceived) + atomic.LoadInt64(op.dataSent)) * 8)
	duplexNSBitRate := duplexBits / float64(timeNeeded)
	bitRate := (duplexNSBitRate / 2) * float64(time.Second)
	op.testResult = int(bitRate)

	// Save the result to the crane.
	if controller, ok := op.Terminal().(*CraneControllerTerminal); ok {
		if controller.Crane.ConnectedHub != nil {
			controller.Crane.ConnectedHub.GetMeasurements().SetCapacity(op.testResult)
			log.Infof(
				"docks: measured capacity to %s: %.2f Mbit/s (%.2fMB down / %.2fMB up in %s)",
				controller.Crane.ConnectedHub,
				float64(op.testResult)/1000000,
				float64(op.dataReceived)/1000000,
				float64(atomic.LoadInt64(op.dataSent))/1000000,
				timeNeeded,
			)
			return nil
		} else if controller.Crane.IsMine() {
			return terminal.ErrInternalError.With("capacity operation was run on %s without a connected hub set", controller.Crane)
		}
	} else if !runningTests {
		return terminal.ErrInternalError.With("capacity operation was run on terminal that is not a crane controller, but %T", op.Terminal())
	}

	return nil
}

// Deliver delivers a message.
func (op *CapacityTestOp) Deliver(msg *terminal.Msg) *terminal.Error {
	// Optimized delivery with 1s timeout.
	select {
	case op.recvQueue <- msg:
	default:
		select {
		case op.recvQueue <- msg:
		case <-time.After(1 * time.Second):
			msg.Finish()
			return terminal.ErrTimeout
		}
	}
	return nil
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *CapacityTestOp) HandleStop(tErr *terminal.Error) (errorToSend *terminal.Error) {
	// Return result to waiting routine.
	select {
	case op.result <- tErr:
	default:
	}

	// Drain the recvQueue to finish the message units.
drain:
	for {
		select {
		case msg := <-op.recvQueue:
			msg.Finish()
		default:
			select {
			case msg := <-op.recvQueue:
				msg.Finish()
			case <-time.After(3 * time.Millisecond):
				// Give some additional time buffer to drain the queue.
				break drain
			}
		}
	}

	// Return error as is.
	return tErr
}

// Result returns the result (end error) of the operation.
func (op *CapacityTestOp) Result() <-chan *terminal.Error {
	return op.result
}
