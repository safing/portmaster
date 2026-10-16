package api

import (
	"testing"
)

// TestReadyEndpoint checks that the ready endpoint reports a ready Portmaster as ready.
func TestReadyEndpoint(t *testing.T) {
	t.Parallel()

	// The test instance reports that Portmaster is ready.
	msg, err := ready(nil)
	if err != nil {
		t.Fatalf("ready endpoint failed although Portmaster is ready: %s", err)
	}
	if msg != "Portmaster is ready." {
		t.Errorf("unexpected response: %q", msg)
	}
}
