package profile

import (
	"errors"
	"strings"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/runtime"
)

const (
	revisionProviderPrefix = "layeredProfile/"
)

var (
	errProfileNotActive = errors.New("profile not active")
	errNoLayeredProfile = errors.New("profile has no layered profile")
)

func registerRevisionProvider() error {
	_, err := runtime.Register(
		revisionProviderPrefix,
		runtime.SimpleValueGetterFunc(getRevisions),
	)
	return err
}

func getRevisions(key string) ([]record.Record, error) {
	key = strings.TrimPrefix(key, revisionProviderPrefix)

	var profiles []*Profile

	if key == "" {
		profiles = getAllActiveProfiles()
	} else {
		// Get active profile.
		profile := getActiveProfile(key)
		if profile == nil {
			return nil, errProfileNotActive
		}
	}

	records := make([]record.Record, 0, len(profiles))

	for _, p := range profiles {
		layered, err := getProfileRevision(p)
		if err != nil {
			log.Warningf("failed to get layered profile for %s: %s", p.ID, err)
			continue
		}

		records = append(records, layered)
	}

	return records, nil
}

// getProfileRevision returns the layered profile for p.
// It also updates the layered profile if required.
func getProfileRevision(p *Profile) (*LayeredProfile, error) {
	// Get layered profile.
	layeredProfile := p.LayeredProfile()
	if layeredProfile == nil {
		return nil, errNoLayeredProfile
	}

	// Update profiles if necessary.
	if layeredProfile.NeedsUpdate() {
		layeredProfile.Update()
	}

	return layeredProfile, nil
}
