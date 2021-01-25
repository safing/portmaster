package profile

import (
	"errors"
	"os"
	"strings"

	"github.com/safing/portbase/database"

	"github.com/safing/portbase/dataroot"

	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"golang.org/x/sync/singleflight"
)

const (
	// UnidentifiedProfileID is the profile ID used for unidentified processes.
	UnidentifiedProfileID = "_unidentified"

	// SystemProfileID is the profile ID used for the system/kernel.
	SystemProfileID = "_system"

	// SystemProfileID is the profile ID used for the Portmaster itself.
	PortmasterProfileID = "_portmaster"
)

var getProfileSingleInflight singleflight.Group

// GetProfile fetches a profile. This function ensures that the loaded profile
// is shared among all callers. You must always supply both the scopedID and
// linkedPath parameters whenever available.
func GetProfile(source profileSource, id, linkedPath string) ( //nolint:gocognit
	profile *Profile,
	err error,
) {
	// Select correct key for single in flight.
	singleInflightKey := linkedPath
	if singleInflightKey == "" {
		singleInflightKey = makeScopedID(source, id)
	}

	p, err, _ := getProfileSingleInflight.Do(singleInflightKey, func() (interface{}, error) {
		var previousVersion *Profile

		// Fetch profile depending on the available information.
		switch {
		case id != "":
			scopedID := makeScopedID(source, id)

			// Get profile via the scoped ID.
			// Check if there already is an active and not outdated profile.
			profile = getActiveProfile(scopedID)
			if profile != nil {
				profile.MarkStillActive()

				if profile.outdated.IsSet() {
					previousVersion = profile
				} else {
					return profile, nil
				}
			}
			// Get from database.
			profile, err = getProfile(scopedID)

			// If we cannot find a profile, check if the request is for a special
			// profile we can create.
			if errors.Is(err, database.ErrNotFound) {
				switch id {
				case UnidentifiedProfileID:
					profile = New(SourceLocal, UnidentifiedProfileID, linkedPath)
					err = nil
				case SystemProfileID:
					profile = New(SourceLocal, SystemProfileID, linkedPath)
					err = nil
				case PortmasterProfileID:
					profile = New(SourceLocal, PortmasterProfileID, linkedPath)
					err = nil
				}
			}

		case linkedPath != "":
			// Search for profile via a linked path.
			// Check if there already is an active and not outdated profile for
			// the linked path.
			profile = findActiveProfile(linkedPath)
			if profile != nil {
				if profile.outdated.IsSet() {
					previousVersion = profile
				} else {
					return profile, nil
				}
			}
			// Get from database.
			profile, err = findProfile(linkedPath)

		default:
			return nil, errors.New("cannot fetch profile without ID or path")
		}
		if err != nil {
			return nil, err
		}

		// Process profiles coming directly from the database.
		// As we don't use any caching, these will be new objects.

		// Add a layeredProfile to local and network profiles.
		if profile.Source == SourceLocal || profile.Source == SourceNetwork {
			// If we are refetching, assign the layered profile from the previous version.
			if previousVersion != nil {
				profile.layeredProfile = previousVersion.layeredProfile
			}

			// Local profiles must have a layered profile, create a new one if it
			// does not yet exist.
			if profile.layeredProfile == nil {
				profile.layeredProfile = NewLayeredProfile(profile)
			}
		}

		// Add the profile to the currently active profiles.
		addActiveProfile(profile)

		return profile, nil
	})
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("profile getter returned nil")
	}

	return p.(*Profile), nil
}

// getProfile fetches the profile for the given scoped ID.
func getProfile(scopedID string) (profile *Profile, err error) {
	// Get profile from the database.
	r, err := profileDB.Get(profilesDBPath + scopedID)
	if err != nil {
		return nil, err
	}

	// Parse and prepare the profile, return the result.
	return prepProfile(r)
}

// findProfile searches for a profile with the given linked path. If it cannot
// find one, it will create a new profile for the given linked path.
func findProfile(linkedPath string) (profile *Profile, err error) {
	// Search the database for a matching profile.
	it, err := profileDB.Query(
		query.New(makeProfileKey(SourceLocal, "")).Where(
			query.Where("LinkedPath", query.SameAs, linkedPath),
		),
	)
	if err != nil {
		return nil, err
	}

	// Only wait for the first result, or until the query ends.
	r := <-it.Next
	// Then cancel the query, should it still be running.
	it.Cancel()

	// Prep and return an existing profile.
	if r != nil {
		profile, err = prepProfile(r)
		return profile, err
	}

	// If there was no profile in the database, create a new one, and return it.
	profile = New(SourceLocal, "", linkedPath)

	// Check if the profile should be marked as internal.
	// This is the case whenever the binary resides within the data root dir.
	if strings.HasPrefix(linkedPath, dataroot.Root().Dir+string(os.PathSeparator)) {
		profile.Internal = true
	}

	return profile, nil
}

func prepProfile(r record.Record) (*Profile, error) {
	// ensure its a profile
	profile, err := EnsureProfile(r)
	if err != nil {
		return nil, err
	}

	// prepare config
	err = profile.prepConfig()
	if err != nil {
		log.Errorf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// parse config
	err = profile.parseConfig()
	if err != nil {
		log.Errorf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// return parsed profile
	return profile, nil
}
