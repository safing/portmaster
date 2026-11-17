package notifications

import "testing"

// TestDeleteOldCopyKeepsReplacement checks that deleting an old notification
// object does not remove a newer notification with the same EventID from the
// notification storage.
//
//nolint:paralleltest // Uses the global notification storage.
func TestDeleteOldCopyKeepsReplacement(t *testing.T) {
	setupTestModule(t)

	const id = "test:replaced-by-newer"
	oldN := Notify(&Notification{EventID: id, Title: "Old", Message: "Old"})
	newN := Notify(&Notification{EventID: id, Title: "New", Message: "New"})
	defer Delete(id)

	if Get(id) != newN {
		t.Fatal("newer notification was not stored")
	}

	oldN.Delete()

	if got := Get(id); got != newN {
		t.Fatalf("newer notification unreachable after deleting old copy: got %v", got)
	}
	if newN.Meta().IsDeleted() {
		t.Fatal("newer notification must not be marked deleted")
	}
}
