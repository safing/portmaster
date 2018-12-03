package profile

import "sync"

var (
	activeProfileSets     = make(map[string]*Set)
	activeProfileSetsLock sync.RWMutex
)

func activateProfileSet(set *Set) {
	set.Lock()
	defer set.Unlock()
	activeProfileSetsLock.Lock()
	defer activeProfileSetsLock.Unlock()
	activeProfileSets[set.profiles[0].ID] = set
}

// DeactivateProfileSet marks a profile set as not active.
func DeactivateProfileSet(set *Set) {
	set.Lock()
	defer set.Unlock()
	activeProfileSetsLock.Lock()
	defer activeProfileSetsLock.Unlock()
	delete(activeProfileSets, set.profiles[0].ID)
}

func updateActiveUserProfile(profile *Profile) {
	activeProfileSetsLock.RLock()
	defer activeProfileSetsLock.RUnlock()
	activeSet, ok := activeProfileSets[profile.ID]
	if ok {
		activeSet.Lock()
		defer activeSet.Unlock()
		activeSet.profiles[0] = profile
	}
}

func updateActiveGlobalProfile(profile *Profile) {
	updateActiveProfile(1, profile)
}

func updateActiveStampProfile(profile *Profile) {
	updateActiveProfile(2, profile)
}

func updateActiveFallbackProfile(profile *Profile) {
	updateActiveProfile(3, profile)
}

func updateActiveProfile(setID int, profile *Profile) {
	activeProfileSetsLock.RLock()
	defer activeProfileSetsLock.RUnlock()

	for _, activeSet := range activeProfileSets {
		activeSet.Lock()
		activeProfile := activeSet.profiles[setID]
		if activeProfile != nil {
			activeProfile.Lock()
			if activeProfile.ID == profile.ID {
				activeSet.profiles[setID] = profile
			}
			activeProfile.Unlock()
		}
		activeSet.Unlock()
	}
}
