package profile

import (
	"context"

	"github.com/hashicorp/go-version"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/migration"
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
	)
}

func migrateNetworkRatingSystem(ctx context.Context, _, _ *version.Version, db *database.Interface) error {
	// determine the default value for the network rating system by searching for
	// a global security level setting that is not set to the default.
	networkRatingEnabled := false
	for _, cfgkey := range securityLevelSettings {
		def, err := config.GetOption(cfgkey)
		if err != nil {
			return err
		}

		intValue := config.Concurrent.GetAsInt(cfgkey, 0)()
		if def.DefaultValue.(uint8) != uint8(intValue) {
			log.Tracer(ctx).Infof("found global security level setting with changed value. 0x%2x (default) != 0x%2x (current)", def.DefaultValue, intValue)
			networkRatingEnabled = true
			break
		}
	}

	if networkRatingEnabled {
		status.SetNetworkRating(networkRatingEnabled)
	}

	return nil
}
