package sluice

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/spn/crew"
)

func newTestSluice() *Sluice {
	// The network name is not registered, so abandoning does not touch the registry.
	return &Sluice{network: "test", pendingRequests: make(map[string]*Request)}
}

func newTestPendingConn(port uint16) *network.Connection {
	return &network.Connection{
		ID:         "test-conn",
		IPVersion:  packet.IPv4,
		IPProtocol: packet.TCP,
		LocalIP:    net.IPv4(127, 0, 0, 1),
		LocalPort:  port,
		Verdict:    network.VerdictRerouteToTunnel,
	}
}

// TestAbandonCallbackHoldsConnLock checks that abandoning a sluice fails the
// pending connections with the connection lock held, so that concurrent
// readers (packet handler, API) do not race with it. Run with -race.
func TestAbandonCallbackHoldsConnLock(t *testing.T) {
	t.Parallel()

	s := newTestSluice()
	conn := newTestPendingConn(50001)
	err := s.AwaitRequest(&Request{ConnInfo: conn, CallbackFn: crew.HandleSluiceRequest})
	if err != nil {
		t.Fatal(err)
	}

	// Concurrent reader, as e.g. the packet handler of the next packet.
	var wg sync.WaitGroup
	wg.Go(func() {
		conn.Lock()
		defer conn.Unlock()
		_ = conn.Verdict
		_ = conn.Reason.Msg
	})

	s.abandon()
	wg.Wait()

	conn.Lock()
	defer conn.Unlock()
	if conn.Verdict != network.VerdictFailed {
		t.Fatalf("expected verdict failed, got %s", conn.Verdict)
	}
}

// TestAbandonDoesNotDeadlockWithAwaitRequest reproduces the lock order of the
// firewall: AwaitRequest is called with the connection lock held. Abandon must
// therefore not hold the sluice lock while the callback takes a connection lock.
func TestAbandonDoesNotDeadlockWithAwaitRequest(t *testing.T) {
	t.Parallel()

	s := newTestSluice()
	pending := newTestPendingConn(50002)
	err := s.AwaitRequest(&Request{ConnInfo: pending, CallbackFn: crew.HandleSluiceRequest})
	if err != nil {
		t.Fatal(err)
	}

	// Firewall: holds the pending connection's lock (e.g. handling its next
	// packet) and waits for the sluice lock to register a request.
	pending.Lock()
	abandonDone := make(chan struct{})
	go func() {
		defer close(abandonDone)
		s.abandon()
	}()
	// Give abandon time to take the sluice lock and block in the callback.
	time.Sleep(50 * time.Millisecond)

	awaitDone := make(chan struct{})
	go func() {
		defer close(awaitDone)
		_ = s.AwaitRequest(&Request{ConnInfo: newTestPendingConn(50003), CallbackFn: crew.HandleSluiceRequest})
		pending.Unlock()
	}()

	select {
	case <-awaitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: AwaitRequest blocked on sluice lock held by abandon")
	}
	select {
	case <-abandonDone:
	case <-time.After(5 * time.Second):
		t.Fatal("abandon did not finish")
	}
}
