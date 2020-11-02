package profile

import (
	"context"
	"sync"
	"time"
)

const (
	activeProfileCleanerTickDuration = 1 * time.Minute
	activeProfileCleanerThreshold    = 5 * time.Minute
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

// findActiveProfile searched for an active local profile using the linked path.
func findActiveProfile(linkedPath string) *Profile {
	activeProfilesLock.RLock()
	defer activeProfilesLock.RUnlock()

	for _, activeProfile := range activeProfiles {
		if activeProfile.LinkedPath == linkedPath {
			activeProfile.MarkStillActive()
			return activeProfile
		}
	}

	return nil
}

// addActiveProfile registers a active profile.
func addActiveProfile(profile *Profile) {
	activeProfilesLock.Lock()
	defer activeProfilesLock.Unlock()

	profile.MarkStillActive()
	activeProfiles[profile.ScopedID()] = profile
}

// markActiveProfileAsOutdated marks an active profile as outdated.
func markActiveProfileAsOutdated(scopedID string) {
	activeProfilesLock.RLock()
	defer activeProfilesLock.RUnlock()

	profile, ok := activeProfiles[scopedID]
	if ok {
		profile.outdated.Set()
	}
}

func cleanActiveProfiles(ctx context.Context) error {
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
