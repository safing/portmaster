package terminal

import (
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

// CounterOpType is the type ID for the Counter Operation.
const CounterOpType string = "debug/count"

// CounterOp sends increasing numbers on both sides.
type CounterOp struct { //nolint:maligned
	OperationBase

	wg     sync.WaitGroup
	server bool
	opts   *CounterOpts

	counterLock   sync.Mutex
	ClientCounter uint64
	ServerCounter uint64
	Error         error
}

// CounterOpts holds the options for CounterOp.
type CounterOpts struct {
	ClientCountTo uint64
	ServerCountTo uint64
	Wait          time.Duration
	Flush         bool

	suppressWorker bool
}

func init() {
	RegisterOpType(OperationFactory{
		Type:  CounterOpType,
		Start: startCounterOp,
	})
}

// NewCounterOp returns a new CounterOp.
func NewCounterOp(t Terminal, opts CounterOpts) (*CounterOp, *Error) {
	// Create operation.
	op := &CounterOp{
		opts: &opts,
	}
	op.wg.Add(1)

	// Create argument container.
	data, err := dsd.Dump(op.opts, dsd.JSON)
	if err != nil {
		return nil, ErrInternalError.With("failed to pack options: %w", err)
	}

	// Initialize operation.
	tErr := t.StartOperation(op, container.New(data), 3*time.Second)
	if tErr != nil {
		return nil, tErr
	}

	// Start worker if needed.
	if op.getRemoteCounterTarget() > 0 && !op.opts.suppressWorker {
		module.mgr.Go("counter sender", op.CounterWorker)
	}
	return op, nil
}

func startCounterOp(t Terminal, opID uint32, data *container.Container) (Operation, *Error) {
	// Create operation.
	op := &CounterOp{
		server: true,
	}
	op.InitOperationBase(t, opID)
	op.wg.Add(1)

	// Parse arguments.
	opts := &CounterOpts{}
	_, err := dsd.Load(data.CompileData(), opts)
	if err != nil {
		return nil, ErrInternalError.With("failed to unpack options: %w", err)
	}
	op.opts = opts

	// Start worker if needed.
	if op.getRemoteCounterTarget() > 0 {
		module.mgr.Go("counter sender", op.CounterWorker)
	}

	return op, nil
}

// Type returns the operation's type ID.
func (op *CounterOp) Type() string {
	return CounterOpType
}

func (op *CounterOp) getCounter(sending, increase bool) uint64 {
	op.counterLock.Lock()
	defer op.counterLock.Unlock()

	// Use server counter, when op is server or for sending, but not when both.
	if op.server != sending {
		if increase {
			op.ServerCounter++
		}
		return op.ServerCounter
	}

	if increase {
		op.ClientCounter++
	}
	return op.ClientCounter
}

func (op *CounterOp) getRemoteCounterTarget() uint64 {
	if op.server {
		return op.opts.ClientCountTo
	}
	return op.opts.ServerCountTo
}

func (op *CounterOp) isDone() bool {
	op.counterLock.Lock()
	defer op.counterLock.Unlock()

	return op.ClientCounter >= op.opts.ClientCountTo &&
		op.ServerCounter >= op.opts.ServerCountTo
}

// Deliver delivers data to the operation.
func (op *CounterOp) Deliver(msg *Msg) *Error {
	defer msg.Finish()

	nextStep, err := msg.Data.GetNextN64()
	if err != nil {
		op.Stop(op, ErrMalformedData.With("failed to parse next number: %w", err))
		return nil
	}

	// Count and compare.
	counter := op.getCounter(false, true)

	// Debugging:
	// if counter < 100 ||
	// 	counter < 1000 && counter%100 == 0 ||
	// 	counter < 10000 && counter%1000 == 0 ||
	// 	counter < 100000 && counter%10000 == 0 ||
	// 	counter < 1000000 && counter%100000 == 0 {
	// 	log.Errorf("spn/terminal: counter %s>%d recvd, now at %d", op.t.FmtID(), op.id, counter)
	// }

	if counter != nextStep {
		log.Warningf(
			"terminal: integrity of counter op violated: received %d, expected %d",
			nextStep,
			counter,
		)
		op.Stop(op, ErrIntegrity.With("counters mismatched"))
		return nil
	}

	// Check if we are done.
	if op.isDone() {
		op.Stop(op, nil)
	}

	return nil
}

// HandleStop handles stopping the operation.
func (op *CounterOp) HandleStop(err *Error) (errorToSend *Error) {
	// Check if counting finished.
	if !op.isDone() {
		err := fmt.Errorf(
			"counter op %d: did not finish counting (%d<-%d %d->%d)",
			op.id,
			op.opts.ClientCountTo, op.ClientCounter,
			op.ServerCounter, op.opts.ServerCountTo,
		)
		op.Error = err
	}

	op.wg.Done()
	return err
}

// SendCounter sends the next counter.
func (op *CounterOp) SendCounter() *Error {
	if op.Stopped() {
		return ErrStopping
	}

	// Increase sending counter.
	counter := op.getCounter(true, true)

	// Debugging:
	// if counter < 100 ||
	// 	counter < 1000 && counter%100 == 0 ||
	// 	counter < 10000 && counter%1000 == 0 ||
	// 	counter < 100000 && counter%10000 == 0 ||
	// 	counter < 1000000 && counter%100000 == 0 {
	// 	defer log.Errorf("spn/terminal: counter %s>%d sent, now at %d", op.t.FmtID(), op.id, counter)
	// }

	return op.Send(op.NewMsg(varint.Pack64(counter)), 3*time.Second)
}

// Wait waits for the Counter Op to finish.
func (op *CounterOp) Wait() {
	op.wg.Wait()
}

// CounterWorker is a worker that sends counters.
func (op *CounterOp) CounterWorker(ctx *mgr.WorkerCtx) error {
	for {
		// Send counter msg.
		err := op.SendCounter()
		switch err {
		case nil:
			// All good, continue.
		case ErrStopping:
			// Done!
			return nil
		default:
			// Something went wrong.
			err := fmt.Errorf("counter op %d: failed to send counter: %w", op.id, err)
			op.Error = err
			op.Stop(op, ErrInternalError.With(err.Error()))
			return nil
		}

		// Maybe flush message.
		if op.opts.Flush {
			op.terminal.Flush(1 * time.Second)
		}

		// Check if we are done with sending.
		if op.getCounter(true, false) >= op.getRemoteCounterTarget() {
			return nil
		}

		// Maybe wait a little.
		if op.opts.Wait > 0 {
			time.Sleep(op.opts.Wait)
		}
	}
}
