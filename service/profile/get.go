package profile

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

var getProfileLock sync.Mutex

// GetLocalProfile fetches a profile. This function ensures that the loaded profile
// is shared among all callers. Always provide all available data points.
// Passing an ID without MatchingData is valid, but could lead to inconsistent
// data - use with caution.
func GetLocalProfile(id string, md MatchingData, createProfileCallback func() *Profile) ( //nolint:gocognit
	profile *Profile,
	err error,
) {
	// Globally lock getting a profile.
	// This does not happen too often, and it ensures we really have integrity
	// and no race conditions.
	getProfileLock.Lock()
	defer getProfileLock.Unlock()

	var previousVersion *Profile

	// Get active profile based on the ID, if available.
	if id != "" {
		// Check if there already is an active profile.
		profile = getActiveProfile(MakeScopedID(SourceLocal, id))
		if profile != nil {
			// Mark active and return if not outdated.
			if profile.outdated.IsNotSet() {
				profile.MarkStillActive()
				return profile, nil
			}

			// If outdated, get from database.
			previousVersion = profile
			profile = nil
		}
	}

	// In some cases, we might need to get a profile directly, without matching data.
	// This could lead to inconsistent data - use with caution.
	// Example: Saving prompt results to profile should always be to the same ID!
	if md == nil {
		if id == "" {
			return nil, errors.New("cannot get local profiles without ID and matching data")
		}

		profile, err = getProfile(MakeScopedID(SourceLocal, id))
		if err != nil {
			return nil, fmt.Errorf("failed to load profile %s by ID: %w", MakeScopedID(SourceLocal, id), err)
		}
	}

	// Check if we are requesting a special profile.
	var created, special bool
	if id != "" && isSpecialProfileID(id) {
		special = true

		// Get special profile from DB.
		if profile == nil {
			profile, err = getProfile(MakeScopedID(SourceLocal, id))
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				log.Warningf("profile: failed to get special profile %s: %s", id, err)
			}
		}

		// Create profile if not found or if it needs a reset.
		if profile == nil || specialProfileNeedsReset(profile) {
			profile = createSpecialProfile(id, md.Path())
			created = true
		}
	}

	// If we don't have a profile yet, find profile based on matching data.
	if profile == nil {
		profile, err = findProfile(SourceLocal, md)
		if err != nil {
			return nil, fmt.Errorf("failed to search for profile: %w", err)
		}
	}

	// If we still don't have a profile, create a new one.
	if profile == nil {
		created = true

		// Try the profile creation callback, if we have one.
		if createProfileCallback != nil {
			profile = createProfileCallback()
		}

		// If that did not work, create a standard profile.
		if profile == nil {
			fpPath := md.MatchingPath()
			if fpPath == "" {
				fpPath = md.Path()
			}

			profile = New(&Profile{
				ID:                  id,
				Source:              SourceLocal,
				PresentationPath:    md.Path(),
				UsePresentationPath: true,
				Fingerprints: []Fingerprint{
					{
						Type:      FingerprintTypePathID,
						Operation: FingerprintOperationEqualsID,
						Value:     fpPath,
					},
				},
			})
		}
	}

	// Initialize and update profile.

	// Update metadata.
	var changed bool
	if md != nil {
		if special {
			changed = updateSpecialProfileMetadata(profile, md.Path())
		} else {
			changed = profile.updateMetadata(md.Path())
		}
	}

	// Save if created or changed.
	if created || changed {
		// Save profile.
		err := profile.Save()
		if err != nil {
			log.Warningf("profile: failed to save profile %s after creation: %s", profile.ScopedID(), err)
		}
	}

	// Trigger further metadata fetching from system if profile was created.
	if created && profile.UsePresentationPath && !special {
		module.mgr.Go("get profile metadata", func(wc *mgr.WorkerCtx) error {
			return profile.updateMetadataFromSystem(wc.Ctx(), md)
		})
	}

	// Prepare profile for first use.

	// Process profiles are coming directly from the database or are new.
	// As we don't use any caching, these will be new objects.

	// Add a layeredProfile.

	// If we are refetching, assign the layered profile from the previous version.
	// The internal references will be updated when the layered profile checks for updates.
	if previousVersion != nil && previousVersion.layeredProfile != nil {
		profile.layeredProfile = previousVersion.layeredProfile
	}

	// Profiles must have a layered profile, create a new one if it
	// does not yet exist.
	if profile.layeredProfile == nil {
		profile.layeredProfile = NewLayeredProfile(profile)
	}

	// Add the profile to the currently active profiles.
	addActiveProfile(profile)

	return profile, nil
}

// getProfile fetches the profile for the given scoped ID.
func getProfile(scopedID string) (profile *Profile, err error) {
	// Get profile from the database.
	r, err := profileDB.Get(ProfilesDBPath + scopedID)
	if err != nil {
		return nil, err
	}

	// Parse and prepare the profile, return the result.
	return loadProfile(r)
}

// findProfile searches for a profile with the given linked path. If it cannot
// find one, it will create a new profile for the given linked path.
func findProfile(source ProfileSource, md MatchingData) (profile *Profile, err error) {
	// TODO: Loading every profile from database and parsing it for every new
	// process might be quite expensive. Measure impact and possibly improve.

	// Get iterator over all profiles.
	it, err := profileDB.Query(query.New(ProfilesDBPath + MakeScopedID(source, "")))
	if err != nil {
		return nil, fmt.Errorf("failed to query for profiles: %w", err)
	}

	// Find best matching profile.
	var (
		highestScore int
		bestMatch    record.Record
	)
profileFeed:
	for r := range it.Next {
		// Parse fingerprints.
		prints, err := loadProfileFingerprints(r)
		if err != nil {
			log.Debugf("profile: failed to load fingerprints of %s: %s", r.Key(), err)
		}
		// Continue with any returned fingerprints.
		if prints == nil {
			continue profileFeed
		}

		// Get matching score and compare.
		score := MatchFingerprints(prints, md)
		switch {
		case score == 0:
			// Continue to next.
		case score > highestScore:
			highestScore = score
			bestMatch = r
		case score == highestScore:
			// Notify user of conflict and abort.
			// Use first match - this should be consistent.
			notifyConflictingProfiles(bestMatch, r, md)
			it.Cancel()
			break profileFeed
		}
	}

	// Check if there was an error while iterating.
	if it.Err() != nil {
		return nil, fmt.Errorf("failed to iterate over profiles: %w", err)
	}

	// Return nothing if no profile matched.
	if bestMatch == nil {
		return nil, nil
	}

	// If we have a match, parse and return the profile.
	profile, err = loadProfile(bestMatch)
	if err != nil {
		return nil, fmt.Errorf("failed to parse selected profile %s: %w", bestMatch.Key(), err)
	}

	// Check if this profile is already active and return the active version instead.
	if activeProfile := getActiveProfile(profile.ScopedID()); activeProfile != nil && !activeProfile.IsOutdated() {
		return activeProfile, nil
	}

	// Return nothing if no profile matched.
	return profile, nil
}

func loadProfileFingerprints(r record.Record) (parsed *ParsedFingerprints, err error) {
	// Ensure it's a profile.
	profile, err := EnsureProfile(r)
	if err != nil {
		return nil, err
	}

	// Parse and return fingerprints.
	return ParseFingerprints(profile.Fingerprints, profile.LinkedPath)
}

func loadProfile(r record.Record) (*Profile, error) {
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

	// Mark as recently seen.
	meta.UpdateLastSeen(profile.ScopedID())

	// return parsed profile
	return profile, nil
}

func notifyConflictingProfiles(a, b record.Record, md MatchingData) {
	// Get profile names.
	var idA, nameA, idB, nameB string
	profileA, err := EnsureProfile(a)
	if err == nil {
		idA = profileA.ScopedID()
		nameA = profileA.Name
	} else {
		idA = strings.TrimPrefix(a.Key(), ProfilesDBPath)
		nameA = path.Base(idA)
	}
	profileB, err := EnsureProfile(b)
	if err == nil {
		idB = profileB.ScopedID()
		nameB = profileB.Name
	} else {
		idB = strings.TrimPrefix(b.Key(), ProfilesDBPath)
		nameB = path.Base(idB)
	}

	// Notify user about conflict.
	notifications.NotifyWarn(
		fmt.Sprintf("profiles:match-conflict:%s:%s", idA, idB),
		"App Settings Match Conflict",
		fmt.Sprintf(
			"Multiple app settings match the app at %q with the same priority, please change on of them: %q or %q",
			md.Path(),
			nameA,
			nameB,
		),
		notifications.Action{
			Text:    "Change (1)",
			Type:    notifications.ActionTypeOpenProfile,
			Payload: idA,
		},
		notifications.Action{
			Text:    "Change (2)",
			Type:    notifications.ActionTypeOpenProfile,
			Payload: idB,
		},
		notifications.Action{
			ID:   "ack",
			Text: "OK",
		},
	)
}
