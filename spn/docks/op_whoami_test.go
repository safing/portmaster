package docks

import (
	"testing"

	"github.com/safing/portmaster/spn/terminal"
)

func TestWhoAmIOp(t *testing.T) {
	t.Parallel()

	// Create test terminal pair.
	a, _, err := terminal.NewSimpleTestTerminalPair(0, 0, nil)
	if err != nil {
		t.Fatalf("failed to create test terminal pair: %s", err)
	}

	// Run op.
	resp, tErr := WhoAmI(a)
	if tErr.IsError() {
		t.Fatal(tErr)
	}
	t.Logf("whoami: %+v", resp)
}
