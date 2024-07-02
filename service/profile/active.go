package profile

import (
	"sync"
	"time"

	"github.com/safing/portmaster/service/mgr"
)

const (
	activeProfileCleanerTickDuration = 5 * time.Minute
	activeProfileCleanerThreshold    = 1 * time.Hour
)

var (
	activeProfiles     = make(map[string]*Profile)
	activeProfilesLock sync.RWMutex
)

// getActiveProfile returns a cached copy of an active profile and
// nil if it isn't found.
func getActiveProfile(scopedID string) *Profile {
	activeProfilesLock.RLock()
	defer activeProfilesLock.RUnlock()

	return activeProfiles[scopedID]
}

// getAllActiveProfiles returns a slice of active profiles.
func getAllActiveProfiles() []*Profile {
	activeProfilesLock.RLock()
	defer activeProfilesLock.RUnlock()

	result := make([]*Profile, 0, len(activeProfiles))
	for _, p := range activeProfiles {
		result = append(result, p)
	}

	return result
}

// addActiveProfile registers a active profile.
func addActiveProfile(profile *Profile) {
	activeProfilesLock.Lock()
	defer activeProfilesLock.Unlock()

	// Mark any previous profile as outdated.
	if previous, ok := activeProfiles[profile.ScopedID()]; ok {
		previous.outdated.Set()
	}

	// Mark new profile active and add to active profiles.
	profile.MarkStillActive()
	activeProfiles[profile.ScopedID()] = profile
}

func cleanActiveProfiles(ctx *mgr.WorkerCtx) error {
	for {
		select {
		case <-time.After(activeProfileCleanerTickDuration):

			threshold := time.Now().Add(-activeProfileCleanerThreshold).Unix()

			activeProfilesLock.Lock()
			for id, profile := range activeProfiles {
				// Remove profile if it hasn't been used for a while.
				if profile.LastActive() < threshold {
					profile.outdated.Set()
					delete(activeProfiles, id)
				}
			}
			activeProfilesLock.Unlock()

		case <-ctx.Done():
			return nil
		}
	}
}
