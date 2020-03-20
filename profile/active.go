package profile

import (
	"sync"
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
		profile.oudated.Set()
		delete(activeProfiles, scopedID)
	}
}
