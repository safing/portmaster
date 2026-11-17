package sluice

import (
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// setTestModule installs a fresh module manager for tests.
func setTestModule(t *testing.T) *mgr.Manager {
	t.Helper()

	m := mgr.New("sluice-test")
	module = &SluiceModule{mgr: m}
	t.Cleanup(func() {
		m.Cancel()
		stopAllSluices()
	})
	return m
}

// TestSluiceStopBeforeListen simulates the module being stopped (Stop and
// Cancel) before the listener worker got to create its listener. The worker
// must still finish and release the listener.
func TestSluiceStopBeforeListen(t *testing.T) { //nolint:paralleltest // Uses global module state.
	m := setTestModule(t)

	s := &Sluice{
		network:         "tcp4",
		address:         "127.0.0.1:0",
		createListener:  net.Listen,
		pendingRequests: make(map[string]*Request),
	}

	// Module stop: Stop() (nothing registered yet), then Cancel().
	stopAllSluices()
	m.Cancel()

	// Worker only starts now.
	m.Go("test sluice listener", s.listenHandler)
	// Wait until the worker has created its listener (and thus is registered
	// as a running worker).
	waitFor(t, func() bool {
		s.lock.Lock()
		defer s.lock.Unlock()
		return s.listener != nil
	})
	done := m.WaitForWorkers(2 * time.Second)
	if !done {
		s.abandon() // Release the leaked worker.
		t.Fatal("sluice listener worker did not stop after module stop")
	}
}

func testConnInfo(ipVersion packet.IPVersion, proto packet.IPProtocol, localIP net.IP, localPort uint16) *network.Connection {
	return &network.Connection{
		IPVersion:  ipVersion,
		IPProtocol: proto,
		LocalIP:    localIP,
		LocalPort:  localPort,
		Entity:     &intel.Entity{},
	}
}

// TestExpiredRequestIsNotUsed checks that an expired pending request neither
// blocks a new request for the same local address nor is handed a new
// connection.
func TestExpiredRequestIsNotUsed(t *testing.T) {
	t.Parallel()

	s := &Sluice{
		network:         "udp4",
		pendingRequests: make(map[string]*Request),
	}
	localIP := net.IPv4(127, 0, 0, 1)
	const key = "127.0.0.1:40000"
	noop := func(*network.Connection, net.Conn) {}

	stale := &Request{
		ConnInfo:   testConnInfo(packet.IPv4, packet.UDP, localIP, 40000),
		CallbackFn: noop,
		Expires:    time.Now().Add(-time.Second),
	}
	err := s.AwaitRequest(stale)
	if err != nil {
		t.Fatal(err)
	}

	// Expired request must not be returned.
	r, ok := s.getRequest(key)
	if ok {
		t.Fatalf("got expired request %p", r)
	}

	// Re-add the stale one and check that a new request replaces it.
	s.pendingRequests[key] = stale
	fresh := &Request{
		ConnInfo:   testConnInfo(packet.IPv4, packet.UDP, localIP, 40000),
		CallbackFn: noop,
	}
	err = s.AwaitRequest(fresh)
	if err != nil {
		t.Fatalf("new request blocked by expired one: %s", err)
	}
	r, ok = s.getRequest(key)
	if !ok || r != fresh {
		t.Fatal("expected fresh request to be returned")
	}

	// A non-expired request must still block duplicates.
	err = s.AwaitRequest(fresh)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AwaitRequest(&Request{ConnInfo: fresh.ConnInfo, CallbackFn: noop})
	if err == nil {
		t.Fatal("expected duplicate request to be rejected")
	}
}
