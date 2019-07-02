package matcher

import (
	"fmt"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/index"
)

// GetProfileSet finds a local profile.
func GetProfileSet(proc *process.Process) (set *profile.ProfileSet, err error) {

	identPath := GetIdentificationPath(proc)
	pi, err := index.GetIndex(identPath)

	var bestScore int
	var bestProfile *profile.Profile

	for _, id := range pi.UserProfiles {
		prof, err := profile.GetUserProfile(id)
		if err != nil {
			log.Errorf("profile/matcher: failed to load profile: %s", err)
			continue
		}

		score, err := CheckFingerprints(proc, prof)
		if score > bestScore {
			bestScore = score
			bestProfile = prof
		}
	}

	if bestProfile == nil {
		bestProfile = ProfileFromProcess(proc)
	}

	// FIXME: fetch stamp profile
	set = profile.NewSet(bestProfile, nil)
	return set, nil
}

// ProfileFromProcess creates an initial profile based on the given process.
func ProfileFromProcess(proc *process.Process) *profile.Profile {
	new := profile.New()

	splittedPath := strings.Split(proc.Path, "/")
	new.Name = strings.ToTitle(splittedPath[len(splittedPath)-1])

	new.Identifiers = append(new.Identifiers, GetIdentificationPath(proc))
	new.Fingerprints = append(new.Fingerprints, fmt.Sprintf("fullpath:%s", proc.Path))

	err := new.Save(profile.UserNamespace)
	if err != nil {
		log.Errorf("profile/matcher: could not save new profile: %s", new.Name)
	}

	return new
}
