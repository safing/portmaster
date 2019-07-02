package matcher

import (
	"strings"

	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
)

// CheckFingerprints checks what fingerprints match and returns the total score.
func CheckFingerprints(proc *process.Process, prof *profile.Profile) (score int, err error) {
	// FIXME: kinda a dummy for now

	for _, fp := range prof.Fingerprints {
		if strings.HasPrefix(fp, "fullpath:") {
			if fp[9:] == proc.Path {
				return 3, nil
			}
		}
	}

	return 0, nil
}
