package crew

import (
	"testing"
	"time"

	"github.com/safing/portmaster/spn/terminal"
)

func TestPingOp(t *testing.T) {
	t.Parallel()

	// Create test terminal pair.
	a, _, err := terminal.NewSimpleTestTerminalPair(0, 0, nil)
	if err != nil {
		t.Fatalf("failed to create test terminal pair: %s", err)
	}

	// Create ping op.
	op, tErr := NewPingOp(a)
	if tErr.IsError() {
		t.Fatal(tErr)
	}

	// Wait for result.
	select {
	case result := <-op.Result:
		t.Logf("ping result: %s", result.Error())
	case <-time.After(pingOpTimeout):
		t.Fatal("timed out")
	}
}
