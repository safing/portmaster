package profile

import "testing"

func TestReloadProfilesWithPendingFilterLists(t *testing.T) {
	t.Parallel()

	pending := &Profile{Source: SourceLocal, ID: "pending-filterlists"}
	pending.prepProfile()
	pending.filterListsPending.Store(true)

	resolved := &Profile{Source: SourceLocal, ID: "resolved-filterlists"}
	resolved.prepProfile()

	addActiveProfile(pending)
	addActiveProfile(resolved)
	t.Cleanup(func() {
		activeProfilesLock.Lock()
		defer activeProfilesLock.Unlock()
		delete(activeProfiles, pending.ScopedID())
		delete(activeProfiles, resolved.ScopedID())
	})

	reloadProfilesWithPendingFilterLists()

	if !pending.IsOutdated() {
		t.Error("profile with pending filter lists should be marked as outdated")
	}
	if resolved.IsOutdated() {
		t.Error("profile with resolved filter lists should not be marked as outdated")
	}
}
