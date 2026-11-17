package docks

import (
	"testing"
	"time"

	"github.com/safing/portmaster/spn/terminal"
)

var (
	testCapacityTestVolume  = 1_000_000
	testCapacitytestMaxTime = 1 * time.Second
)

func TestCapacityOp(t *testing.T) { //nolint:paralleltest // Performance test.
	// Defaults.
	testCapacityOp(t, &CapacityTestOptions{
		TestVolume: testCapacityTestVolume,
		MaxTime:    testCapacitytestMaxTime,
		testing:    true,
	})

	// Hit max time first.
	testCapacityOp(t, &CapacityTestOptions{
		TestVolume: testCapacityTestVolume,
		MaxTime:    100 * time.Millisecond,
		testing:    true,
	})

	// Hit volume first.
	testCapacityOp(t, &CapacityTestOptions{
		TestVolume: 100_000,
		MaxTime:    testCapacitytestMaxTime,
		testing:    true,
	})
}

func testCapacityOp(t *testing.T, opts *CapacityTestOptions) {
	t.Helper()

	var (
		capTestDelay            = 5 * time.Millisecond
		capTestQueueSize uint32 = 10
	)

	// Create test terminal pair.
	a, b, err := terminal.NewSimpleTestTerminalPair(
		capTestDelay,
		int(capTestQueueSize),
		&terminal.TerminalOpts{
			FlowControl:     terminal.FlowControlDFQ,
			FlowControlSize: capTestQueueSize,
		},
	)
	if err != nil {
		t.Fatalf("failed to create test terminal pair: %s", err)
	}

	// Grant permission for op on remote terminal and start op.
	b.GrantPermission(terminal.IsCraneController)
	op, tErr := NewCapacityTestOp(a, opts)
	if tErr != nil {
		t.Fatalf("failed to start op: %s", tErr)
	}

	// Wait for result and check error.
	tErr = <-op.Result()
	if !tErr.IsOK() {
		t.Fatalf("op failed: %s", tErr)
	}
	t.Logf("measured capacity: %d bit/s", op.testResult)

	// Calculate expected bandwidth.
	expectedBitsPerSecond := float64(capacityTestMsgSize*8*int64(capTestQueueSize)) / float64(capTestDelay) * float64(time.Second)
	t.Logf("expected capacity: %f bit/s", expectedBitsPerSecond)

	// Check if measured bandwidth is within parameters.
	if float64(op.testResult) > expectedBitsPerSecond*1.6 {
		t.Fatal("measured capacity too high")
	}
	// TODO: Check if we can raise this to at least 90%.
	if float64(op.testResult) < expectedBitsPerSecond*0.2 {
		t.Fatal("measured capacity too low")
	}
}

// TestCapacityOpRemoteStop checks that the capacity test operations end
// promptly when the remote side stops, instead of running until their own
// timeouts.
func TestCapacityOpRemoteStop(t *testing.T) { //nolint:paralleltest // Uses the global capacity test lock.
	// Create test terminal pair.
	a, b, err := terminal.NewSimpleTestTerminalPair(
		5*time.Millisecond,
		10,
		&terminal.TerminalOpts{
			FlowControl:     terminal.FlowControlDFQ,
			FlowControlSize: 10,
		},
	)
	if err != nil {
		t.Fatalf("failed to create test terminal pair: %s", err)
	}
	defer a.Abandon(nil)

	// Start a test that would run for a long time.
	b.GrantPermission(terminal.IsCraneController)
	op, tErr := NewCapacityTestOp(a, &CapacityTestOptions{
		TestVolume: maxCapacityTestVolume,
		MaxTime:    maxCapacityTestMaxTime,
		testing:    true,
	})
	if tErr != nil {
		t.Fatalf("failed to start op: %s", tErr)
	}

	// Wait for the test to be running on both sides.
	time.Sleep(500 * time.Millisecond)
	if n := countGoroutinesIn(t, "(*CapacityTestOp).handler"); n != 2 {
		t.Fatalf("expected 2 running capacity op handlers, found %d", n)
	}

	// Stop the test from the remote side by abandoning the remote terminal.
	b.Abandon(nil)
	select {
	case tErr = <-op.Result():
		t.Logf("op ended with: %s", tErr)
	case <-time.After(5 * time.Second):
		t.Fatal("op did not end after remote stop")
	}

	// All handlers and senders must end promptly.
	started := time.Now()
	waitUntil := started.Add(5 * time.Second)
	for countGoroutinesIn(t, "(*CapacityTestOp).handler")+countGoroutinesIn(t, "(*CapacityTestOp).sender") > 0 {
		if time.Now().After(waitUntil) {
			t.Fatalf("capacity op workers still running %s after remote stop", time.Since(started))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("capacity op workers ended %s after remote stop", time.Since(started))

	// The global capacity test lock must be released.
	if capacityTestRunning.IsSet() {
		t.Fatal("capacity test lock still set after all workers ended")
	}
}
