package profile

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/go-version"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/migration"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/profile/binmeta"
)

func registerMigrations() error {
	return migrations.Add(
		migration.Migration{
			Description: "Migrate from LinkedPath to Fingerprints and PresentationPath",
			Version:     "v0.9.9",
			MigrateFunc: migrateLinkedPath,
		},
		migration.Migration{
			Description: "Migrate from Icon Fields to Icon List",
			Version:     "v1.4.7",
			MigrateFunc: migrateIcons,
		},
		migration.Migration{
			Description: "Migrate from random profile IDs to fingerprint-derived IDs",
			Version:     "v1.6.3", // Re-run after mixed results in v1.6.0
			MigrateFunc: migrateToDerivedIDs,
		},
	)
}

func migrateLinkedPath(ctx context.Context, _, to *version.Version, db *database.Interface) error {
	// Get iterator over all profiles.
	it, err := db.Query(query.New(ProfilesDBPath))
	if err != nil {
		log.Tracer(ctx).Errorf("profile: failed to migrate from linked path: failed to start query: %s", err)
		return nil
	}

	// Migrate all profiles.
	for r := range it.Next {
		// Parse profile.
		profile, err := EnsureProfile(r)
		if err != nil {
			log.Tracer(ctx).Debugf("profiles: failed to parse profile %s for migration: %s", r.Key(), err)
			continue
		}

		// Skip if there is no LinkedPath to migrate from.
		if profile.LinkedPath == "" {
			continue
		}

		// Update metadata and save if changed.
		if profile.updateMetadata("") {
			err = db.Put(profile)
			if err != nil {
				log.Tracer(ctx).Debugf("profiles: failed to save profile %s after migration: %s", r.Key(), err)
			} else {
				log.Tracer(ctx).Tracef("profiles: migrated profile %s to %s", r.Key(), to)
			}
		}
	}

	// Check if there was an error while iterating.
	if err := it.Err(); err != nil {
		log.Tracer(ctx).Errorf("profile: failed to migrate from linked path: failed to iterate over profiles for migration: %s", err)
	}

	return nil
}

func migrateIcons(ctx context.Context, _, to *version.Version, db *database.Interface) error {
	// Get iterator over all profiles.
	it, err := db.Query(query.New(ProfilesDBPath))
	if err != nil {
		log.Tracer(ctx).Errorf("profile: failed to migrate from icon fields: failed to start query: %s", err)
		return nil
	}

	// Migrate all profiles.
	var (
		lastErr error
		failed  int
		total   int
	)
	for r := range it.Next {
		// Parse profile.
		profile, err := EnsureProfile(r)
		if err != nil {
			log.Tracer(ctx).Debugf("profiles: failed to parse profile %s for migration: %s", r.Key(), err)
			continue
		}

		// Skip if there is no (valid) icon defined or the icon list is already populated.
		if profile.Icon == "" || profile.IconType == "" || len(profile.Icons) > 0 {
			continue
		}

		// Migrate to icon list.
		profile.Icons = []binmeta.Icon{{
			Type:  profile.IconType,
			Value: profile.Icon,
		}}

		// Save back to DB.
		err = db.Put(profile)
		if err != nil {
			failed++
			lastErr = err
			log.Tracer(ctx).Debugf("profiles: failed to save profile %s after migration: %s", r.Key(), err)
		} else {
			log.Tracer(ctx).Tracef("profiles: migrated profile %s to %s", r.Key(), to)
		}
		total++
	}

	// Check if there was an error while iterating.
	if err := it.Err(); err != nil {
		log.Tracer(ctx).Errorf("profile: failed to migrate from icon fields: failed to iterate over profiles for migration: %s", err)
	}

	// Log migration failure and try again next time.
	if lastErr != nil {
		// Normally, an icon migration would not be such a big error, but this is a test
		// run for the profile IDs and we absolutely need to know if anything went wrong.
		module.states.Add(mgr.State{
			ID:      "migration-failed-icons",
			Name:    "Profile Migration Failed",
			Message: fmt.Sprintf("Failed to migrate icons of %d profiles (out of %d pending). The last error was: %s\n\nPlease restart Portmaster to try the migration again.", failed, total, lastErr),
			Type:    mgr.StateTypeError,
		})
		return fmt.Errorf("failed to migrate %d profiles (out of %d pending) - last error: %w", failed, total, lastErr)
	}

	return lastErr
}

var randomUUIDRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func migrateToDerivedIDs(ctx context.Context, _, to *version.Version, db *database.Interface) error {
	var profilesToDelete []string //nolint:prealloc // We don't know how many profiles there are.

	// Get iterator over all profiles.
	it, err := db.Query(query.New(ProfilesDBPath))
	if err != nil {
		log.Tracer(ctx).Errorf("profile: failed to migrate to derived profile IDs: failed to start query: %s", err)
		return nil
	}

	// Migrate all profiles.
	var (
		lastErr error
		failed  int
		total   int
	)
	for r := range it.Next {
		// Parse profile.
		profile, err := EnsureProfile(r)
		if err != nil {
			failed++
			lastErr = err
			log.Tracer(ctx).Debugf("profiles: failed to parse profile %s for migration: %s", r.Key(), err)
			continue
		}

		// Skip if the ID does not look like a random UUID.
		if !randomUUIDRegex.MatchString(profile.ID) {
			continue
		}

		// Generate new ID.
		oldScopedID := profile.ScopedID()
		newID := DeriveProfileID(profile.Fingerprints)

		// If they match, skip migration for this profile.
		if profile.ID == newID {
			continue
		}

		// Reset key.
		profile.ResetKey()
		// Set new ID and rebuild the key.
		profile.ID = newID
		profile.makeKey()

		// Save back to DB.
		err = db.Put(profile)
		if err != nil {
			failed++
			lastErr = err
			log.Tracer(ctx).Debugf("profiles: failed to save profile %s after migration: %s", r.Key(), err)
		} else {
			log.Tracer(ctx).Tracef("profiles: migrated profile %s to %s", r.Key(), to)

			// Add old ID to profiles that we need to delete.
			profilesToDelete = append(profilesToDelete, oldScopedID)
		}
		total++
	}

	// Check if there was an error while iterating.
	if err := it.Err(); err != nil {
		log.Tracer(ctx).Errorf("profile: failed to migrate to derived profile IDs: failed to iterate over profiles for migration: %s", err)
	}

	// Delete old migrated profiles.
	for _, scopedID := range profilesToDelete {
		if err := db.Delete(ProfilesDBPath + scopedID); err != nil {
			log.Tracer(ctx).Errorf("profile: failed to delete old profile %s during migration: %s", scopedID, err)
		}
	}

	// Log migration failure and try again next time.
	if lastErr != nil {
		module.states.Add(mgr.State{
			ID:      "migration-failed-derived-IDs",
			Name:    "Profile Migration Failed",
			Message: fmt.Sprintf("Failed to migrate profile IDs of %d profiles (out of %d pending). The last error was: %s\n\nPlease restart Portmaster to try the migration again.", failed, total, lastErr),
			Type:    mgr.StateTypeError,
		})
		return fmt.Errorf("failed to migrate %d profiles (out of %d pending) - last error: %w", failed, total, lastErr)
	}

	return nil
}
