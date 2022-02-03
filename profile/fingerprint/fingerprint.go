package profile

var fingerprintWeights = map[string]int{
	"full_path":    2,
	"partial_path": 1,
	"md5_sum":      4,
	"sha1_sum":     5,
	"sha256_sum":   6,
}

// Fingerprint links processes to profiles.
type Fingerprint struct {
	OS       string
	Type     string
	Value    string
	Comment  string
	LastUsed int64
}

// MatchesOS returns whether the Fingerprint is applicable for the current OS.
func (fp *Fingerprint) MatchesOS() bool {
	return fp.OS == osIdentifier
}

// GetFingerprintWeight returns the weight of the given fingerprint type.
func GetFingerprintWeight(fpType string) (weight int) {
	weight, ok := fingerprintWeights[fpType]
	if ok {
		return weight
	}
	return 0
}

// TODO: move to profile
/*
// AddFingerprint adds the given fingerprint to the profile.
func (profile *Profile) AddFingerprint(fp *Fingerprint) {
	if fp.OS == "" {
		fp.OS = osIdentifier
	}
	if fp.LastUsed == 0 {
		fp.LastUsed = time.Now().Unix()
	}

	profile.Fingerprints = append(profile.Fingerprints, fp)
}
*/

// TODO: matching
/*
//nolint:deadcode,unused // FIXME
func matchProfile(p *Process, prof *profile.Profile) (score int) {
	for _, fp := range prof.Fingerprints {
		score += matchFingerprint(p, fp)
	}
	return
}

//nolint:deadcode,unused // FIXME
func matchFingerprint(p *Process, fp *profile.Fingerprint) (score int) {
	if !fp.MatchesOS() {
		return 0
	}

	switch fp.Type {
	case "full_path":
		if p.Path == fp.Value {
			return profile.GetFingerprintWeight(fp.Type)
		}
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
*/
