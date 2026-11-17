package access_test

import (
	"testing"

	"github.com/safing/portmaster/spn/access"
)

// TestAccessStubNoOp verifies the access stub starts and stops cleanly.
func TestAccessStubNoOp(t *testing.T) {
	t.Parallel()
	a, err := access.New(nil)
	if err != nil {
		t.Fatalf("access.New() returned unexpected error: %v", err)
	}
	err = a.Start()
	if err != nil {
		t.Fatalf("Access.Start() returned unexpected error: %v", err)
	}
	err = a.Stop()
	if err != nil {
		t.Fatalf("Access.Stop() returned unexpected error: %v", err)
	}
}

// TestAccessStubNotLoggedIn verifies no account session is present.
func TestAccessStubNotLoggedIn(t *testing.T) {
	t.Parallel()
	a, err := access.New(nil)
	if err != nil {
		t.Fatalf("access.New() returned unexpected error: %v", err)
	}
	if a.IsLoggedIn() {
		t.Error("expected IsLoggedIn() to return false — account login has been removed")
	}
}

// TestAccessStubAllFeaturesUnlocked verifies all features are available without a subscription.
func TestAccessStubAllFeaturesUnlocked(t *testing.T) {
	t.Parallel()
	a, err := access.New(nil)
	if err != nil {
		t.Fatalf("access.New() returned unexpected error: %v", err)
	}
	features := []string{"bandwidth-visibility", "network-history", "spn", "filter-lists", "custom-lists"}
	for _, f := range features {
		if !a.HasFeature(f) {
			t.Errorf("expected HasFeature(%q) to return true, got false", f)
		}
	}
}

// TestAccessStubManagerIsNil verifies no background workers are spawned.
func TestAccessStubManagerIsNil(t *testing.T) {
	t.Parallel()
	a, err := access.New(nil)
	if err != nil {
		t.Fatalf("access.New() returned unexpected error: %v", err)
	}
	if a.Manager() != nil {
		t.Error("expected Manager() to return nil for no-op stub, got non-nil")
	}
}
