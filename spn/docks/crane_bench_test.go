package docks

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

const benchSinkOpType = "debug/docks-bench-sink"

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:  benchSinkOpType,
		Start: startBenchSinkOp,
	})
}

// benchSinkOp receives a fixed number of data messages and then signals done.
type benchSinkOp struct {
	terminal.OperationBase

	remaining uint64
	done      chan struct{}
}

func (op *benchSinkOp) Type() string { return benchSinkOpType }

func (op *benchSinkOp) Deliver(msg *terminal.Msg) *terminal.Error {
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

func startBenchSinkOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	n, err := data.GetNextN64()
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to get msg count: %w", err)
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
	terminal.OperationBase
}

func (op *benchSourceOp) Type() string { return benchSinkOpType }

func (op *benchSourceOp) Deliver(msg *terminal.Msg) *terminal.Error {
	msg.Finish()
	return nil
}

// benchSinkTerminal is a crane terminal replacement that just counts messages.
type benchSinkTerminal struct {
	terminal.BareTerminal

	id        uint32
	crane     *Crane
	remaining int
	done      chan struct{}
}

func (t *benchSinkTerminal) ID() uint32           { return t.id }
func (t *benchSinkTerminal) Ctx() context.Context { return module.mgr.Ctx() }
func (t *benchSinkTerminal) FmtID() string        { return fmt.Sprintf("bench-%d", t.id) }

func (t *benchSinkTerminal) Abandon(_ *terminal.Error) {
	t.crane.deleteTerminal(t.id)
}

func (t *benchSinkTerminal) Deliver(msg *terminal.Msg) *terminal.Error {
	msg.Finish()
	// Deliver is only called by the crane handler, so no locking is needed.
	t.remaining--
	if t.remaining == 0 {
		close(t.done)
	}
	return nil
}

func startBenchCranes(b *testing.B, encrypt bool) (crane1, crane2 *Crane) {
	b.Helper()

	var identity *cabin.Identity
	var connectedHub *hub.Hub
	if encrypt {
		if testIdentity == nil {
			var err error
			testIdentity, err = cabin.CreateIdentity(module.mgr.Ctx(), "test")
			if err != nil {
				b.Fatalf("failed to create identity: %s", err)
			}
		}
		identity, connectedHub = testIdentity, testIdentity.Hub
	}

	// Use realistic load sizes. Tests may modify this global.
	optimalMinLoadSize = 3072
	ship := ships.NewTestShip(!encrypt, ships.BaseMTU)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		var err error
		crane1, err = NewCrane(ship, connectedHub, nil)
		if err == nil {
			err = crane1.Start(module.mgr.Ctx())
		}
		if err != nil {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		crane2, err = NewCrane(ship.Reverse(), nil, identity)
		if err == nil {
			err = crane2.Start(module.mgr.Ctx())
		}
		if err != nil {
			errs <- err
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		b.Fatalf("failed to start cranes: %s", err)
	}

	return crane1, crane2
}

func stopBenchCranes(crane1, crane2 *Crane) {
	crane1.Stop(nil)
	crane2.Stop(nil)
}

// BenchmarkCraneThroughput pumps terminal messages through a crane pair over
// the in-memory test ship. It measures the crane loader (framing, padding,
// encryption), the unloader and the handler (segment parsing, delivery).
func BenchmarkCraneThroughput(b *testing.B) {
	for _, encrypt := range []bool{false, true} {
		for _, size := range []int{100, 1400} {
			b.Run(fmt.Sprintf("encrypt=%v/size=%d", encrypt, size), func(b *testing.B) {
				crane1, crane2 := startBenchCranes(b, encrypt)
				defer stopBenchCranes(crane1, crane2)

				sink := &benchSinkTerminal{
					id:        8,
					crane:     crane2,
					remaining: b.N,
					done:      make(chan struct{}),
				}
				crane2.setTerminal(sink)

				payload := make([]byte, size)
				for i := range payload {
					payload[i] = byte(i)
				}

				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()

				for range b.N {
					msg := terminal.NewMsg(payload)
					msg.FlowID = sink.id
					tErr := crane1.Send(msg, 3*time.Second)
					if tErr != nil {
						b.Fatal(tErr)
					}
				}

				select {
				case <-sink.done:
				case <-time.After(60 * time.Second):
					b.Fatal("timed out waiting for sink")
				}
				b.StopTimer()
			})
		}
	}
}

// BenchmarkCraneTerminalThroughput pumps operation messages through a crane
// terminal pair (home hub options, with flow control) on top of a crane pair
// over the in-memory test ship. This is the full per message path of the SPN
// stack below the operations.
func BenchmarkCraneTerminalThroughput(b *testing.B) {
	for _, encrypt := range []bool{false, true} {
		for _, size := range []int{100, 1400} {
			b.Run(fmt.Sprintf("encrypt=%v/size=%d", encrypt, size), func(b *testing.B) {
				crane1, crane2 := startBenchCranes(b, encrypt)
				defer stopBenchCranes(crane1, crane2)

				// Establish crane terminal pair.
				localTerm, initData, tErr := NewLocalCraneTerminal(crane1, nil, terminal.DefaultHomeHubTerminalOpts())
				if tErr != nil {
					b.Fatal(tErr)
				}
				tErr = crane1.EstablishNewTerminal(localTerm, initData)
				if tErr != nil {
					b.Fatal(tErr)
				}

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
				tErr = localTerm.StartOperation(src, container.New(varint.Pack64(msgCount)), 3*time.Second)
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
				localTerm.Flush(10 * time.Second)

				select {
				case <-done:
				case <-time.After(60 * time.Second):
					b.Fatal("timed out waiting for sink")
				}
				b.StopTimer()
			})
		}
	}
}
