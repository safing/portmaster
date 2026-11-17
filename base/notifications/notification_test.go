package notifications

import (
	"fmt"
	"sync"
	"testing"
)

var testModuleOnce sync.Once

func setupTestModule(t *testing.T) {
	t.Helper()

	testModuleOnce.Do(func() {
		_, err := New(struct{}{})
		if err != nil {
			t.Fatalf("failed to create notifications module: %s", err)
		}
	})
}

// TestApplyUpdateConcurrentDelete checks that a user response that is applied
// while the notification is deleted (e.g. by the expiry cleaner) does not
// access the notification without holding its lock and does not re-add the
// deleted notification to the notification storage.
//
//nolint:paralleltest // Uses the global notification storage.
func TestApplyUpdateConcurrentDelete(t *testing.T) {
	setupTestModule(t)

	var zombies int
	defer func() {
		if zombies > 0 {
			t.Errorf("%d deleted notifications were re-added to the notification storage", zombies)
		}
	}()

	for i := range 1000 {
		id := fmt.Sprintf("test:concurrent-delete-%d", i)
		n := Notify(&Notification{
			EventID: id,
			Title:   "Test",
			Message: "Test",
		})

		var wg sync.WaitGroup
		wg.Go(func() {
			_, _ = applyUpdate(&Notification{EventID: id, SelectedActionID: "ack"}, id)
		})
		wg.Go(func() {
			n.delete(true)
		})
		wg.Wait()

		// The deletion must be final, regardless of the order.
		if zombie := Get(id); zombie != nil {
			zombies++
		}

		// Clean up.
		Delete(id)
	}
}
