package profile

import (
	"fmt"
	"testing"
)

// TestLayeredProfileUnlockForUsageDuringUpdate checks that UnlockForUsage
// releases exactly the layers that LockForUsage locked, even if Update
// replaces an outdated layer concurrently (as happens when multiple
// connections of the same process are handled by the firewall).
//
//nolint:paralleltest // Uses the global active profiles.
func TestLayeredProfileUnlockForUsageDuringUpdate(t *testing.T) {
	for i := range 200 {
		id := fmt.Sprintf("test-unlock-for-usage-%d", i)
		oldProfile := New(&Profile{ID: id, Source: SourceLocal})
		addActiveProfile(oldProfile)
		lp := NewLayeredProfile(oldProfile)

		// A connection uses the layered profile.
		lp.LockForUsage()

		// Meanwhile, the profile is updated (this marks the old one as outdated)
		// and another connection updates the layered profile.
		newProfile := New(&Profile{ID: id, Source: SourceLocal})
		addActiveProfile(newProfile)
		updated := make(chan struct{})
		go func() {
			defer close(updated)
			lp.Update(nil, nil)
		}()

		// The first connection is done.
		lp.UnlockForUsage()
		<-updated

		// All locks of the old layer must be released.
		if !oldProfile.TryLock() {
			t.Fatalf("iteration %d: replaced layer is still read-locked", i)
		}
		oldProfile.Unlock()

		// The new layer was never locked for usage and must be unlocked too.
		if !newProfile.TryLock() {
			t.Fatalf("iteration %d: new layer is locked", i)
		}
		newProfile.Unlock()

		// Clean up.
		activeProfilesLock.Lock()
		delete(activeProfiles, newProfile.ScopedID())
		activeProfilesLock.Unlock()
	}
}
