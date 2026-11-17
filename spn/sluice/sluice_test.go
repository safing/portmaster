package sluice

import (
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/service/mgr"
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
