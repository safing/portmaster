package process

import (
	"fmt"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/profile"
	"github.com/Safing/portmaster/profile/index"
)

// FindProfiles finds and assigns a profile set to the process.
func (p *Process) FindProfiles() error {

	// Get fingerprints of process

	// Check if user profile already exists, else create new
	pathIdentifier := profile.GetPathIdentifier(p.Path)
	indexRecord, err := index.Get(pathIdentifier)
	if err != nil && err != database.ErrNotFound {
		log.Errorf("process: could not get profile index for %s: %s", pathIdentifier, err)
	}

	var possibleProfiles []*profile.Profile
	if indexRecord != nil {
		for _, profileID := range indexRecord.UserProfiles {
			prof, err := profile.Get(profileID)
			if err != nil {
				log.Errorf("process: failed to load profile %s: %s", profileID, err)
			}
				possibleProfiles = append(possibleProfiles, prof)
			}
		}
	}

	prof := selectProfile(p, possibleProfiles)
	if prof == nil {
		// create new profile
		prof := profile.New()
		prof.Name = p.ExecName
		prof.AddFingerprint(&profile.Fingerprint{
			Type:  "full_path",
			Value: p.Path,
		})
		// TODO: maybe add sha256_sum?
		prof.MarkUsed()
		prof.Save()
	}

	// Find/Re-evaluate Stamp profile
	// 1. check linked stamp profile
	// 2. if last check is was more than a week ago, fetch from stamp:
	// 3. send path identifier to stamp
	// 4. evaluate all returned profiles
	// 5. select best
	// 6. link stamp profile to user profile
	// FIXME: implement!

	if prof.MarkUsed() {
		prof.Save()
	}

	p.UserProfileKey = prof.Key()
	p.profileSet = profile.NewSet(prof, nil)
	p.Save()

	return nil
}

func selectProfile(p *Process, profs []*profile.Profile) (selectedProfile *profile.Profile) {
	var highestScore int
	for _, prof := range profs {
		score := matchProfile(p, prof)
		if score > highestScore {
			selectedProfile = prof
		}
	}
	return
}

func matchProfile(p *Process, prof *profile.Profile) (score int) {
	for _, fp := range prof.Fingerprints {
		score += matchFingerprint(p, fp)
	}
	return
}

func matchFingerprint(p *Process, fp *profile.Fingerprint) (score int) {
	if !fp.MatchesOS() {
		return 0
	}

	switch fp.Type {
	case "full_path":
		if p.Path == fp.Value {
		}
		return profile.GetFingerprintWeight(fp.Type)
	case "partial_path":
		// FIXME: if full_path matches, do not match partial paths
		return profile.GetFingerprintWeight(fp.Type)
	case "md5_sum", "sha1_sum", "sha256_sum":
		// FIXME: one sum is enough, check sums in a grouped form, start with the best
		sum, err := p.GetExecHash(fp.Type)
		if err != nil {
			log.Errorf("process: failed to get hash of executable: %s", err)
		} else if sum == fp.Value {
			return profile.GetFingerprintWeight(fp.Type)
		}
	}

	return 0
}
