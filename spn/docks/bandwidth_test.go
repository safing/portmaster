package docks

import (
	"testing"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

func TestEffectiveBandwidth(t *testing.T) { //nolint:paralleltest // Run alone.
	// Skip in CI.
	if testing.Short() {
		t.Skip()
	}

	var (
		bwTestDelay            = 50 * time.Millisecond
		bwTestQueueSize uint32 = 1000
		bwTestVolume           = 10000000 // 10MB
		bwTestTime             = 10 * time.Second
	)

	// Create test terminal pair.
	a, b, err := terminal.NewSimpleTestTerminalPair(
		bwTestDelay,
		int(bwTestQueueSize),
		&terminal.TerminalOpts{
			FlowControl:     terminal.FlowControlDFQ,
			FlowControlSize: bwTestQueueSize,
		},
	)
	if err != nil {
		t.Fatalf("failed to create test terminal pair: %s", err)
	}

	// Grant permission for op on remote terminal and start op.
	b.GrantPermission(terminal.IsCraneController)

	// Re-use the capacity test for the bandwidth test.
	op := &CapacityTestOp{
		opts: &CapacityTestOptions{
			TestVolume: bwTestVolume,
			MaxTime:    bwTestTime,
			testing:    true,
		},
		recvQueue:       make(chan *terminal.Msg),
		dataSent:        new(int64),
		dataSentWasAckd: abool.New(),
		result:          make(chan *terminal.Error, 1),
	}
	// Disable sender again.
	op.senderStarted = true
	op.dataSentWasAckd.Set()
	// Make capacity test request.
	request, err := dsd.Dump(op.opts, dsd.CBOR)
	if err != nil {
		t.Fatal(terminal.ErrInternalError.With("failed to serialize capactity test options: %w", err))
	}
	// Send test request.
	tErr := a.StartOperation(op, container.New(request), 1*time.Second)
	if tErr != nil {
		t.Fatal(tErr)
	}
	// Start handler.
	module.mgr.Go("op capacity handler", op.handler)

	// Wait for result and check error.
	tErr = <-op.Result()
	if !tErr.IsOK() {
		t.Fatalf("op failed: %s", tErr)
	}
	t.Logf("measured capacity: %d bit/s", op.testResult)

	// Calculate expected bandwidth.
	expectedBitsPerSecond := (float64(capacityTestMsgSize*8*int64(bwTestQueueSize)) / float64(bwTestDelay)) * float64(time.Second)
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
