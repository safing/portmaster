package profile

import (
	"context"
	"sync"
	"time"
)

const (
	activeProfileCleanerTickDuration = 10 * time.Minute
	activeProfileCleanerThreshold    = 1 * time.Hour
)

var (
	// TODO: periodically clean up inactive profiles
	activeProfiles     = make(map[string]*Profile)
	activeProfilesLock sync.RWMutex
)

// getActiveProfile returns a cached copy of an active profile and nil if it isn't found.
func getActiveProfile(scopedID string) *Profile {
	activeProfilesLock.Lock()
	defer activeProfilesLock.Unlock()

	profile, ok := activeProfiles[scopedID]
	if ok {
		return profile
	}

	return nil
}

// markProfileActive registers a profile as active.
func markProfileActive(profile *Profile) {
	activeProfilesLock.Lock()
	defer activeProfilesLock.Unlock()

	activeProfiles[profile.ScopedID()] = profile
}

// markActiveProfileAsOutdated marks an active profile as outdated, so that it will be refetched from the database.
func markActiveProfileAsOutdated(scopedID string) {
	activeProfilesLock.Lock()
	defer activeProfilesLock.Unlock()

	profile, ok := activeProfiles[scopedID]
	if ok {
		profile.outdated.Set()
		delete(activeProfiles, scopedID)
	}
}

func cleanActiveProfiles(ctx context.Context) error {
	for {
		select {
		case <-time.After(activeProfileCleanerTickDuration):

			threshold := time.Now().Add(-activeProfileCleanerThreshold)

			activeProfilesLock.Lock()
			for id, profile := range activeProfiles {
				// get last used
				profile.Lock()
				lastUsed := profile.lastUsed
				profile.Unlock()
				// remove if not used for a while
				if lastUsed.Before(threshold) {
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
