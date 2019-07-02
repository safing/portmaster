package profile

import (
	"context"
	"sync"

	"github.com/safing/portbase/log"
)

var (
	activeProfileSets     = make(map[string]*Set)
	activeProfileSetsLock sync.RWMutex
)

func activateProfileSet(ctx context.Context, set *Set) {
	activeProfileSetsLock.Lock()
	defer activeProfileSetsLock.Unlock()
	set.Lock()
	defer set.Unlock()
	activeProfileSets[set.id] = set
	log.Tracer(ctx).Tracef("profile: activated profile set %s", set.id)
}

// DeactivateProfileSet marks a profile set as not active.
func DeactivateProfileSet(set *Set) {
	activeProfileSetsLock.Lock()
	defer activeProfileSetsLock.Unlock()
	set.Lock()
	defer set.Unlock()
	delete(activeProfileSets, set.id)
	log.Tracef("profile: deactivated profile set %s", set.id)
}

func updateActiveProfile(profile *Profile, userProfile bool) {
	activeProfileSetsLock.RLock()
	defer activeProfileSetsLock.RUnlock()

	var activeProfile *Profile
	var profilesUpdated bool

	// iterate all active profile sets
	for _, activeSet := range activeProfileSets {
		activeSet.Lock()

		if userProfile {
			activeProfile = activeSet.profiles[0]
		} else {
			activeProfile = activeSet.profiles[2]
		}

		// check if profile exists (for stamp profiles)
		if activeProfile != nil {
			activeProfile.Lock()

			// check if the stamp profile has the same ID
			if activeProfile.ID == profile.ID {
				if userProfile {
					activeSet.profiles[0] = profile
					log.Infof("profile: updated active user profile %s (%s)", profile.ID, profile.LinkedPath)
				} else {
					activeSet.profiles[2] = profile
					log.Infof("profile: updated active stamp profile %s", profile.ID)
				}
				profilesUpdated = true
			}

			activeProfile.Unlock()
		}

		activeSet.Unlock()
	}

	if profilesUpdated {
		increaseUpdateVersion()
	}
}
