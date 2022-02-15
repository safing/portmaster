package profile

import (
	"errors"
	"sync"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
)

var getProfileLock sync.Mutex

// GetProfile fetches a profile. This function ensures that the loaded profile
// is shared among all callers. You must always supply both the scopedID and
// linkedPath parameters whenever available. The linkedPath is used as the key
// for locking concurrent requests, so it must be supplied if available.
// If linkedPath is not supplied, source and id make up the key instead.
func GetProfile(source profileSource, id, linkedPath string, reset bool) ( //nolint:gocognit
	profile *Profile,
	err error,
) {
	// Globally lock getting a profile.
	// This does not happen too often, and it ensures we really have integrity
	// and no race conditions.
	getProfileLock.Lock()
	defer getProfileLock.Unlock()

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

			if profile.outdated.IsSet() || reset {
				previousVersion = profile
			} else {
				return profile, nil
			}
		}

		// Get from database.
		if !reset {
			profile, err = getProfile(scopedID)
			// Check if the profile is special and needs a reset.
			if err == nil && specialProfileNeedsReset(profile) {
				profile = getSpecialProfile(id, linkedPath)
			}
		} else {
			// Simulate missing profile to create new one.
			err = database.ErrNotFound
		}

	case linkedPath != "":
		// Search for profile via a linked path.
		// Check if there already is an active and not outdated profile for
		// the linked path.
		profile = findActiveProfile(linkedPath)
		if profile != nil {
			if profile.outdated.IsSet() || reset {
				previousVersion = profile
			} else {
				return profile, nil
			}
		}

		// Get from database.
		if !reset {
			profile, err = findProfile(linkedPath)
			// Check if the profile is special and needs a reset.
			if err == nil && specialProfileNeedsReset(profile) {
				profile = getSpecialProfile(id, linkedPath)
			}
		} else {
			// Simulate missing profile to create new one.
			err = database.ErrNotFound
		}

	default:
		return nil, errors.New("cannot fetch profile without ID or path")
	}

	// Create new profile if none was found.
	if errors.Is(err, database.ErrNotFound) {
		err = nil

		// Check if there is a special profile for this ID.
		profile = getSpecialProfile(id, linkedPath)

		// If not, create a standard profile.
		if profile == nil {
			profile = New(SourceLocal, id, linkedPath, nil)
		}
	}

	// If there was a non-recoverable error, return here.
	if err != nil {
		return nil, err
	}

	// Process profiles are coming directly from the database or are new.
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
	profile = New(SourceLocal, "", linkedPath, nil)

	return profile, nil
}

func prepProfile(r record.Record) (*Profile, error) {
	// ensure its a profile
	profile, err := EnsureProfile(r)
	if err != nil {
		return nil, err
	}

	// prepare profile
	profile.prepProfile()

	// parse config
	err = profile.parseConfig()
	if err != nil {
		log.Errorf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// Set saved internally to suppress outdating profiles if saving internally.
	profile.savedInternally = true

	// return parsed profile
	return profile, nil
}
