package profile

import "time"

var (
	fingerprintWeights = map[string]int{
		"full_path":    2,
		"partial_path": 1,
		"md5_sum":      4,
		"sha1_sum":     5,
		"sha256_sum":   6,
	}
)

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
