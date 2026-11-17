package profile

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/base/config"
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

const testLayeredOptionKey = "test/layeredProfileOption"

var registerTestLayeredOption sync.Once

// newTestLayeredProfileVersion creates and activates a new version of the
// test profile with the given ID, which marks the previous one as outdated.
func newTestLayeredProfileVersion(t *testing.T, id string, optionValue bool) *Profile {
	t.Helper()

	registerTestLayeredOption.Do(func() {
		err := config.Register(&config.Option{
			Name:         "Test Layered Profile Option",
			Key:          testLayeredOptionKey,
			Description:  "Option for testing the wrapped options of layered profiles.",
			OptType:      config.OptTypeBool,
			DefaultValue: false,
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	p := New(&Profile{
		ID:     id,
		Source: SourceLocal,
		Config: map[string]any{testLayeredOptionKey: optionValue},
	})
	addActiveProfile(p)
	return p
}

func removeTestLayeredProfile(id string) {
	activeProfilesLock.Lock()
	defer activeProfilesLock.Unlock()
	delete(activeProfiles, MakeScopedID(SourceLocal, id))
}

// TestLayeredProfileOptionsWithoutLock checks that the wrapped config options
// may be used without holding a lock on the layered profile (as eg. the
// tunnel checks, the history cleaner and the connection saving do), while the
// layered profile is updated.
// Run with -race.
//
//nolint:paralleltest // Uses the global active profiles.
func TestLayeredProfileOptionsWithoutLock(t *testing.T) {
	const id = "test-options-without-lock"
	defer removeTestLayeredProfile(id)

	lp := NewLayeredProfile(newTestLayeredProfileVersion(t, id, false))
	globalDefault := func() bool {
		t.Error("the option is set by the profile, the global config must not be used")
		return false
	}
	boolOption := lp.wrapBoolOption(testLayeredOptionKey, globalDefault)
	intOption := lp.wrapIntOption("test/unsetIntOption", func() int64 { return 42 })
	stringOption := lp.wrapStringOption("test/unsetStringOption", func() string { return "global" })

	// Use the options without any lock.
	var (
		wg   sync.WaitGroup
		done = make(chan struct{})
	)
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				boolOption()
				if v := intOption(); v != 42 {
					t.Errorf("int option is %d, expected 42", v)
					return
				}
				if v := stringOption(); v != "global" {
					t.Errorf("string option is %q, expected global", v)
					return
				}
			}
		})
	}

	// Meanwhile, replace the layer and bump the revision.
	for i := range 500 {
		value := i%2 == 0
		newTestLayeredProfileVersion(t, id, value)
		lp.Update(nil, nil)
		lp.increaseRevisionCounter(true)

		// After an update, the option must reflect the new layer.
		if got := boolOption(); got != value {
			t.Fatalf("iteration %d: option is %v after update, expected %v", i, got, value)
		}
	}
	close(done)
	wg.Wait()
}

// TestLayeredProfileOptionsDoNotLock checks that the wrapped config options
// do not lock the layered profile: Callers such as MatchFilterLists use them
// while holding a read lock. Acquiring another read lock would deadlock as
// soon as Update waits for the write lock in between.
//
//nolint:paralleltest // Uses the global active profiles.
func TestLayeredProfileOptionsDoNotLock(t *testing.T) {
	const id = "test-options-do-not-lock"
	defer removeTestLayeredProfile(id)

	lp := NewLayeredProfile(newTestLayeredProfileVersion(t, id, true))
	option := lp.wrapBoolOption(testLayeredOptionKey, func() bool { return false })

	// A connection uses the layered profile.
	lp.LockForUsage()

	// Meanwhile, an update waits for the write lock.
	newTestLayeredProfileVersion(t, id, false)
	updated := make(chan struct{})
	go func() {
		defer close(updated)
		lp.Update(nil, nil)
	}()
	// New read locks are refused as soon as a writer is waiting.
	for lp.TryRLock() {
		lp.RUnlock()
		time.Sleep(time.Millisecond)
	}

	// Using an option now must not block and return the value of the
	// revision that is locked for usage.
	result := make(chan bool, 1)
	go func() { result <- option() }()
	select {
	case value := <-result:
		if !value {
			t.Error("option changed while the layered profile was locked for usage")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("option blocks while a read lock is held and an update is waiting")
	}

	lp.UnlockForUsage()
	<-updated
	if option() {
		t.Error("option was not updated")
	}
}
