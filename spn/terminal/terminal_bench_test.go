package terminal

import (
	"fmt"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

const benchSinkOpType = "debug/bench-sink"

func init() {
	RegisterOpType(OperationFactory{
		Type:  benchSinkOpType,
		Start: startBenchSinkOp,
	})
}

// benchSinkOp receives a fixed number of data messages and then signals done.
type benchSinkOp struct {
	OperationBase

	remaining uint64
	done      chan struct{}
}

func (op *benchSinkOp) Type() string {
	return benchSinkOpType
}

func (op *benchSinkOp) Deliver(msg *Msg) *Error {
	msg.Finish()
	// Deliver is only called by the terminal handler, so no locking is needed.
	op.remaining--
	if op.remaining == 0 {
		close(op.done)
	}
	return nil
}

// benchSinkDone is used to pass the done channel of the remote sink to the
// benchmark.
var benchSinkDone = make(chan chan struct{}, 1)

func startBenchSinkOp(t Terminal, opID uint32, data *container.Container) (Operation, *Error) {
	n, err := data.GetNextN64()
	if err != nil {
		return nil, ErrMalformedData.With("failed to get msg count: %w", err)
	}
	op := &benchSinkOp{
		remaining: n,
		done:      make(chan struct{}),
	}
	op.InitOperationBase(t, opID)
	benchSinkDone <- op.done
	return op, nil
}

// benchSourceOp sends data messages.
type benchSourceOp struct {
	OperationBase
}

func (op *benchSourceOp) Type() string {
	return benchSinkOpType
}

func (op *benchSourceOp) Deliver(msg *Msg) *Error {
	msg.Finish()
	return nil
}

// BenchmarkTerminalThroughput pumps data messages through a connected
// terminal pair (in-memory, no crane) and measures the per message cost of
// framing, flow control, unit scheduling and optionally encryption.
func BenchmarkTerminalThroughput(b *testing.B) {
	for _, encrypt := range []bool{false, true} {
		for _, size := range []int{100, 1400} {
			b.Run(fmt.Sprintf("encrypt=%v/size=%d", encrypt, size), func(b *testing.B) {
				benchmarkTerminalThroughput(b, encrypt, size)
			})
		}
	}
}

func benchmarkTerminalThroughput(b *testing.B, encrypt bool, size int) {
	b.Helper()

	opts := DefaultExpansionTerminalOpts()
	opts.Encrypt = encrypt
	opts.FlowControlSize = opts.FlowControl.DefaultSize()

	var a, z *TestTerminal
	var initData *container.Container
	var tErr *Error
	if encrypt {
		identity := getBenchIdentity(b)
		a, initData, tErr = NewLocalTestTerminal(
			module.mgr.Ctx(), 127, "a", identity.Hub, opts, UpstreamSendFunc(func(msg *Msg, _ time.Duration) *Error {
				return z.Deliver(msg)
			}),
		)
		if tErr != nil {
			b.Fatal(tErr)
		}
		z, _, tErr = NewRemoteTestTerminal(
			module.mgr.Ctx(), 127, "z", identity, initData, UpstreamSendFunc(func(msg *Msg, _ time.Duration) *Error {
				return a.Deliver(msg)
			}),
		)
		if tErr != nil {
			b.Fatal(tErr)
		}
	} else {
		var err error
		a, z, err = NewSimpleTestTerminalPair(0, 0, opts)
		if err != nil {
			b.Fatal(err)
		}
	}
	defer func() {
		a.Abandon(nil)
		z.Abandon(nil)
	}()

	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()

	// Start sink on the other side.
	src := &benchSourceOp{}
	msgCount := uint64(b.N) //nolint:gosec // b.N is positive.
	tErr = a.StartOperation(src, container.New(varint.Pack64(msgCount)), 3*time.Second)
	if tErr != nil {
		b.Fatal(tErr)
	}
	done := <-benchSinkDone

	for range b.N {
		tErr = src.Send(src.NewMsg(payload), 3*time.Second)
		if tErr != nil {
			b.Fatal(tErr)
		}
	}
	a.Flush(10 * time.Second)

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		b.Fatal("timed out waiting for sink")
	}
	b.StopTimer()
}

var benchIdentity *cabin.Identity

func getBenchIdentity(b *testing.B) *cabin.Identity {
	b.Helper()

	if benchIdentity == nil {
		var err error
		benchIdentity, err = cabin.CreateIdentity(module.mgr.Ctx(), "test")
		if err != nil {
			b.Fatalf("failed to create identity: %s", err)
		}
	}
	return benchIdentity
}
