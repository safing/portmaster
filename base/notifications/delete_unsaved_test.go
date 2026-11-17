package notifications

import "testing"

// TestDeleteNeverSaved checks that deleting a notification that was never
// saved does not panic.
//
//nolint:paralleltest // Uses the global notification storage.
func TestDeleteNeverSaved(t *testing.T) {
	setupTestModule(t)

	n := &Notification{EventID: "test:never-saved", Title: "T", Message: "M"}
	expired := n.Expired()
	n.Delete()
	n.Delete() // Must also be safe to repeat.

	select {
	case <-expired:
	default:
		t.Error("expired channel was not closed")
	}
	if Get("test:never-saved") != nil {
		t.Error("never-saved notification must not be in storage")
	}
}
