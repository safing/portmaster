package docks

import (
	"bytes"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/terminal"
)

// countGoroutinesIn returns the number of goroutines that are currently in
// the given function.
func countGoroutinesIn(t *testing.T, function string) int {
	t.Helper()

	var buf bytes.Buffer
	err := pprof.Lookup("goroutine").WriteTo(&buf, 2)
	if err != nil {
		t.Fatalf("failed to get goroutine dump: %s", err)
	}
	return strings.Count(buf.String(), function+"(")
}

// abandonProceduresRunning returns the number of goroutines currently running
// a terminal abandon procedure.
func abandonProceduresRunning(t *testing.T) int {
	t.Helper()

	return countGoroutinesIn(t, "(*TerminalBase).handleAbandonProcedure")
}

// TestExpandToFailureCleansUpPromptly checks that a failed ExpandTo does not
// leave the half-built expansion terminal lingering in its abandon procedure.
func TestExpandToFailureCleansUpPromptly(t *testing.T) { //nolint:paralleltest // Inspects all goroutines.
	// Create a terminal to expand from and shut it down, so that starting the
	// expand operation on it fails.
	from, _, tErr := terminal.NewLocalTestTerminal(
		module.mgr.Ctx(), 1, "expand-fail-test", nil,
		terminal.DefaultExpansionTerminalOpts(),
		terminal.UpstreamSendFunc(func(msg *terminal.Msg, _ time.Duration) *terminal.Error {
			msg.Finish()
			return nil
		}),
	)
	if tErr != nil {
		t.Fatalf("failed to create test terminal: %s", tErr)
	}
	from.Abandon(nil)
	select {
	case <-from.Ctx().Done():
	case <-time.After(10 * time.Second):
		t.Fatal("test terminal did not shut down")
	}
	// Wait for the abandon procedure of the test terminal to finish.
	waitUntil := time.Now().Add(10 * time.Second)
	for abandonProceduresRunning(t) > 0 {
		if time.Now().After(waitUntil) {
			t.Fatal("abandon procedure of test terminal did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Expanding must fail.
	expansion, tErr := ExpandTo(from, "expand-fail-test-dst", nil)
	if tErr == nil {
		expansion.Abandon(nil)
		t.Fatal("expanding via a stopped terminal should fail")
	}

	// The half-built expansion terminal must be cleaned up promptly.
	started := time.Now()
	waitUntil = started.Add(10 * time.Second)
	time.Sleep(10 * time.Millisecond) // Give the abandon procedure time to start.
	for abandonProceduresRunning(t) > 0 {
		if time.Now().After(waitUntil) {
			t.Fatalf("abandon procedure of failed expansion terminal still running after %s", time.Since(started))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("failed expansion terminal cleaned up after %s", time.Since(started))
}
