package profile

import (
	"fmt"
	"sync"

	"github.com/safing/portbase/database/record"
)

// MergeProfiles merges multiple profiles into a new one.
// The new profile is saved and returned.
// Only the icon and fingerprints are inherited from other profiles.
// All other information is taken only from the primary profile.
func MergeProfiles(primary *Profile, secondaries ...*Profile) (newProfile *Profile, err error) {
	// Fill info from primary profile.
	newProfile = &Profile{
		Base:                record.Base{},
		RWMutex:             sync.RWMutex{},
		ID:                  "", // Omit ID to derive it from the new fingerprints.
		Source:              primary.Source,
		Name:                primary.Name,
		Description:         primary.Description,
		Homepage:            primary.Homepage,
		UsePresentationPath: false, // Disable presentation path.
		SecurityLevel:       primary.SecurityLevel,
		Config:              primary.Config,
	}

	// Collect all icons.
	newProfile.Icons = make([]Icon, 0, len(secondaries)+1) // Guess the needed space.
	newProfile.Icons = append(newProfile.Icons, primary.Icons...)
	for _, sp := range secondaries {
		newProfile.Icons = append(newProfile.Icons, sp.Icons...)
	}
	newProfile.Icons = sortAndCompactIcons(newProfile.Icons)

	// Collect all fingerprints.
	newProfile.Fingerprints = make([]Fingerprint, 0, len(secondaries)+1) // Guess the needed space.
	newProfile.Fingerprints = addFingerprints(newProfile.Fingerprints, primary.Fingerprints, primary.ScopedID())
	for _, sp := range secondaries {
		newProfile.Fingerprints = addFingerprints(newProfile.Fingerprints, sp.Fingerprints, sp.ScopedID())
	}
	newProfile.Fingerprints = sortAndCompactFingerprints(newProfile.Fingerprints)

	// Save new profile.
	newProfile = New(newProfile)
	err = newProfile.Save()
	if err != nil {
		return nil, fmt.Errorf("failed to save merged profile: %w", err)
	}
	// FIXME: Should we ... ?
	// newProfile.updateMetadata()
	// newProfile.updateMetadataFromSystem()

	// Delete all previous profiles.
	// FIXME:
	/*
		primary.Meta().Delete()
		// Set as outdated and remove from active profiles.
		// Signify that profile was deleted and save for sync.
		for _, sp := range secondaries {
			sp.Meta().Delete()
			// Set as outdated and remove from active profiles.
			// Signify that profile was deleted and save for sync.
		}
	*/

	return newProfile, nil
}

func addFingerprints(existing, add []Fingerprint, from string) []Fingerprint {
	// Copy all fingerprints and add the they are from.
	for _, addFP := range add {
		existing = append(existing, Fingerprint{
			Type:       addFP.Type,
			Key:        addFP.Key,
			Operation:  addFP.Operation,
			Value:      addFP.Value,
			MergedFrom: from,
		})
	}

	return existing
}
