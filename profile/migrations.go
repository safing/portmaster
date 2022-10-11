package profile

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-version"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/migration"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/status"
)

func registerMigrations() error {
	return migrations.Add(
		migration.Migration{
			Description: "Migrate to configurable network rating system",
			Version:     "v0.7.19",
			MigrateFunc: migrateNetworkRatingSystem,
		},
		migration.Migration{
			Description: "Migrate from LinkedPath to Fingerprints and PresentationPath",
			Version:     "v0.9.9",
			MigrateFunc: migrateLinkedPath,
		},
	)
}

func migrateNetworkRatingSystem(ctx context.Context, _, to *version.Version, db *database.Interface) error {
	// determine the default value for the network rating system by searching for
	// a global security level setting that is not set to the default.
	networkRatingEnabled := false
	for _, cfgkey := range securityLevelSettings {
		def, err := config.GetOption(cfgkey)
		if err != nil {
			return err
		}

		intValue := config.Concurrent.GetAsInt(cfgkey, 0)()
		defaultValue, ok := def.DefaultValue.(uint8)
		if ok && defaultValue != uint8(intValue) {
			log.Tracer(ctx).Infof("found global security level setting with changed value. 0x%2x (default) != 0x%2x (current)", def.DefaultValue, intValue)
			networkRatingEnabled = true
			break
		}
	}

	if networkRatingEnabled {
		err := status.SetNetworkRating(networkRatingEnabled)
		if err != nil {
			log.Warningf("profile: migration to %s failed to set network rating level to %v", to, networkRatingEnabled)
		}
	}

	return nil
}

func migrateLinkedPath(ctx context.Context, _, to *version.Version, db *database.Interface) error {
	// Get iterator over all profiles.
	it, err := db.Query(query.New(profilesDBPath))
	if err != nil {
		return fmt.Errorf("failed to query profiles: %w", err)
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
	if it.Err() != nil {
		return fmt.Errorf("profiles: failed to iterate over profiles for migration: %w", err)
	}

	return nil
}
