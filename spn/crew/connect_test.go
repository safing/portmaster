package crew

import (
	"errors"
	"testing"

	"github.com/safing/portmaster/spn/terminal"
)

// TestAbortedAuthError checks that an aborted authentication always results in
// a usable error, also if the operation was stopped without an error.
func TestAbortedAuthError(t *testing.T) {
	t.Parallel()

	// A nil *terminal.Error stays nil when wrapped and does not report an error.
	var nilErr *terminal.Error
	if nilErr.Wrap("test") != nil || nilErr.IsError() || nilErr.Is(terminal.ErrExplicitAck) {
		t.Fatal("unexpected nil receiver behavior")
	}

	// The aborted error must always be a real error that is wrapped properly.
	for _, tErr := range []*terminal.Error{nil, terminal.ErrStopping} {
		wrapped := abortedAuthError(tErr).Wrap("authentication aborted: %w", tErr)
		if wrapped == nil {
			t.Fatalf("aborted error for %v is nil", tErr)
		}
		if wrapped.Error() == "" { // Panics on a nil error.
			t.Fatalf("aborted error for %v has no message", tErr)
		}
	}

	// An error reported by the operation must be preserved.
	if !abortedAuthError(terminal.ErrStopping).Is(terminal.ErrStopping) {
		t.Fatal("error of the aborted operation was replaced")
	}
	// A missing error must become an unknown error.
	if !errors.Is(abortedAuthError(nil), terminal.ErrUnknownError) {
		t.Fatal("missing error was not replaced with an unknown error")
	}
}
