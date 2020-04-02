package profile

import (
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
)

// FindOrCreateLocalProfileByPath returns an existing or new profile for the given application path.
func FindOrCreateLocalProfileByPath(fullPath string) (profile *Profile, new bool, err error) {
	// find local profile
	it, err := profileDB.Query(
		query.New(makeProfileKey(SourceLocal, "")).Where(
			query.Where("LinkedPath", query.SameAs, fullPath),
		),
	)
	if err != nil {
		return nil, false, err
	}

	// get first result
	r := <-it.Next
	// cancel immediately
	it.Cancel()

	// return new if none was found
	if r == nil {
		profile = New()
		profile.LinkedPath = fullPath
		return profile, true, nil
	}

	// ensure its a profile
	profile, err = EnsureProfile(r)
	if err != nil {
		return nil, false, err
	}

	// prepare config
	err = profile.prepConfig()
	if err != nil {
		log.Warningf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// parse config
	err = profile.parseConfig()
	if err != nil {
		log.Warningf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// mark active
	markProfileActive(profile)

	// return parsed profile
	return profile, false, nil
}
