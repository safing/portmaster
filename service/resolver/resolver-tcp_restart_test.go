package resolver

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/mgr"
)

// TestTCPResolverConnHandlerRestartAfterPanic checks that a connection
// handler that is restarted by the worker manager after a panic does not
// panic again on the connection that the first run already shut down.
func TestTCPResolverConnHandlerRestartAfterPanic(t *testing.T) {
	t.Parallel()

	clientSide, serverSide := net.Pipe()
	defer func() {
		_ = serverSide.Close()
	}()
	trc := &tcpResolverConn{
		conn:            &dns.Conn{Conn: clientSide},
		resolverInfo:    &ResolverInfo{Name: "PipeRestartTest", Type: ServerTypeTCP},
		queries:         make(chan *tcpQuery, 10),
		responses:       make(chan *dns.Msg, 10),
		inFlightQueries: make(map[uint16]*tcpQuery, 10),
		inFlightTimeout: time.Minute,
		heartbeat:       make(chan struct{}),
		abandoned:       abool.New(),
		stopped:         make(chan struct{}),
	}

	// Run the handler as getOrCreateResolverConn does, recording each run and
	// whether it panicked. The worker manager recovers a panic and restarts
	// the worker after a backoff (2s for the first restart).
	var (
		lock     sync.Mutex
		runs     int
		finished int
		panics   []any
	)
	module.mgr.Go("test dns client restart", func(w *mgr.WorkerCtx) error {
		lock.Lock()
		runs++
		lock.Unlock()
		defer func() {
			r := recover()
			lock.Lock()
			if r != nil {
				panics = append(panics, r)
			} else {
				finished++
			}
			lock.Unlock()
			if r != nil {
				panic(r)
			}
		}()
		return trc.handler(w)
	})

	// Make the handler panic: the missing Query of a query is dereferenced.
	trc.queries <- &tcpQuery{Response: make(chan *dns.Msg)}
	select {
	case <-trc.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("connection did not stop after the handler panicked")
	}

	// The restarted handler must finish without panicking.
	deadline := time.Now().Add(10 * time.Second)
	for {
		lock.Lock()
		gotRuns, gotFinished, gotPanics := runs, finished, panics
		lock.Unlock()
		if len(gotPanics) > 1 {
			t.Fatalf("restarted handler panicked again (run %d): %v", gotRuns, gotPanics[1:])
		}
		if gotFinished > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("restarted handler did not finish (runs %d, panics %v)", gotRuns, gotPanics)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
