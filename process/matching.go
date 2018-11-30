package process

import (
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/profile"
)

// FindProfiles finds and assigns a profile set to the process.
func (p *Process) FindProfiles() {

	// Get fingerprints of process

	// Check if user profile already exists, else create new

	// Find/Re-evaluate Stamp profile

	// p.UserProfileKey
	// p.profileSet

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
		return profile.GetFingerprintWeight(fp.Type)
	case "md5_sum", "sha1_sum", "sha256_sum":
		sum, err := p.GetExecHash(fp.Type)
		if err != nil {
			log.Errorf("process: failed to get hash of executable: %s", err)
		} else if sum == fp.Value {
			return profile.GetFingerprintWeight(fp.Type)
		}
	}

	return 0
}
