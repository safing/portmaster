package profile

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile/binmeta"
)

// MergeProfiles merges multiple profiles into a new one.
// The new profile is saved and returned.
// Only the icon and fingerprints are inherited from other profiles.
// All other information is taken only from the primary profile.
func MergeProfiles(name string, primary *Profile, secondaries ...*Profile) (newProfile *Profile, err error) {
	if primary == nil || len(secondaries) == 0 {
		return nil, errors.New("must supply both a primary and at least one secondary profile for merging")
	}

	// Fill info from primary profile.
	nowUnix := time.Now().Unix()
	newProfile = &Profile{
		Base:                record.Base{},
		RWMutex:             sync.RWMutex{},
		ID:                  "", // Omit ID to derive it from the new fingerprints.
		Source:              primary.Source,
		Name:                name,
		Description:         primary.Description,
		Homepage:            primary.Homepage,
		UsePresentationPath: false, // Disable presentation path.
		Config:              primary.Config,
		Created:             nowUnix,
	}

	// Fall back to name of primary profile, if none is set.
	if newProfile.Name == "" {
		newProfile.Name = primary.Name
	}

	// If any profile was edited, set LastEdited to now.
	if primary.LastEdited > 0 {
		newProfile.LastEdited = nowUnix
	} else {
		for _, sp := range secondaries {
			if sp.LastEdited > 0 {
				newProfile.LastEdited = nowUnix
				break
			}
		}
	}

	// Collect all icons.
	newProfile.Icons = make([]binmeta.Icon, 0, len(secondaries)+1) // Guess the needed space.
	newProfile.Icons = append(newProfile.Icons, primary.Icons...)
	for _, sp := range secondaries {
		newProfile.Icons = append(newProfile.Icons, sp.Icons...)
	}
	newProfile.Icons = binmeta.SortAndCompactIcons(newProfile.Icons)

	// Collect all fingerprints.
	newProfile.Fingerprints = make([]Fingerprint, 0, len(primary.Fingerprints)+len(secondaries)) // Guess the needed space.
	newProfile.Fingerprints = addFingerprints(newProfile.Fingerprints, primary.Fingerprints, primary.ScopedID())
	for _, sp := range secondaries {
		newProfile.Fingerprints = addFingerprints(newProfile.Fingerprints, sp.Fingerprints, sp.ScopedID())
	}
	newProfile.Fingerprints = sortAndCompactFingerprints(newProfile.Fingerprints)

	// Save new profile.
	newProfile = New(newProfile)
	if err := newProfile.Save(); err != nil {
		return nil, fmt.Errorf("failed to save merged profile: %w", err)
	}

	// Delete all previous profiles.
	if err := primary.delete(); err != nil {
		return nil, fmt.Errorf("failed to delete primary profile %s: %w", primary.ScopedID(), err)
	}
	module.EventMigrated.Submit([]string{primary.ScopedID(), newProfile.ScopedID()})
	for _, sp := range secondaries {
		if err := sp.delete(); err != nil {
			return nil, fmt.Errorf("failed to delete secondary profile %s: %w", sp.ScopedID(), err)
		}
		module.EventMigrated.Submit([]string{sp.ScopedID(), newProfile.ScopedID()})
	}

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

// migrateProfileOnFingerprintChange creates a new profile whose ID is derived
// from the updated fingerprints, copies all settings from the old profile,
// deletes the old profile, and emits EventMigrated. This mirrors the pattern
// used by MergeProfiles and ensures that:
//   - history DB entries are migrated (via EventMigrated → netquery handler)
//   - active connections are re-attributed (via EventDelete → reAttributeConnections)
//   - future connections find the profile via normal fingerprint matching
func migrateProfileOnFingerprintChange(old *Profile) error {
	newDerivedID := DeriveProfileID(old.Fingerprints)

	// Abort if a profile with the target ID already exists — the user may have
	// set fingerprints that conflict with another existing profile.
	_, existsErr := profileDB.Get(ProfilesDBPath + MakeScopedID(old.Source, newDerivedID))
	if existsErr == nil {
		log.Debugf("profile: skipping rename of %s: target ID %s already exists", old.ScopedID(), newDerivedID)
		return nil
	}
	if !errors.Is(existsErr, database.ErrNotFound) {
		return fmt.Errorf("failed to check for existing profile %s: %w", newDerivedID, existsErr)
	}

	// Build the new profile. ID is left empty so New() derives it from Fingerprints.
	newProfile := New(&Profile{
		Source:              old.Source,
		Name:                old.Name,
		Description:         old.Description,
		Warning:             old.Warning,
		WarningLastUpdated:  old.WarningLastUpdated,
		Homepage:            old.Homepage,
		Icon:                old.Icon,
		IconType:            old.IconType,
		Icons:               old.Icons,
		LinkedPath:          old.LinkedPath,
		PresentationPath:    old.PresentationPath,
		UsePresentationPath: old.UsePresentationPath,
		Fingerprints:        old.Fingerprints,
		Config:              old.Config,
		LastEdited:          time.Now().Unix(),
		Internal:            old.Internal,
	})
	// Preserve the original creation timestamp (New() always overwrites it).
	newProfile.Created = old.Created

	if err := newProfile.Save(); err != nil {
		return fmt.Errorf("failed to save renamed profile: %w", err)
	}

	if err := old.delete(); err != nil {
		return fmt.Errorf("failed to delete old profile %s: %w", old.ScopedID(), err)
	}

	module.EventMigrated.Submit([]string{old.ScopedID(), newProfile.ScopedID()})
	log.Infof("profile: renamed profile %q from %s to %s due to fingerprint change",
		old.Name, old.ScopedID(), newProfile.ScopedID())

	return nil
}
