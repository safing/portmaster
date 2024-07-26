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
