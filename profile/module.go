package profile

import (
	"errors"
	"fmt"
	"os"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/migration"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	_ "github.com/safing/portmaster/core/base"
	"github.com/safing/portmaster/updates"
)

var (
	migrations  = migration.New("core:migrations/profile")
	module      *modules.Module
	updatesPath string
)

// Events.
const (
	ConfigChangeEvent = "profile config change"
	DeletedEvent      = "profile deleted"
	MigratedEvent     = "profile migrated"
)

func init() {
	module = modules.Register("profiles", prep, start, stop, "base", "updates")
	module.RegisterEvent(ConfigChangeEvent, true)
	module.RegisterEvent(DeletedEvent, true)
	module.RegisterEvent(MigratedEvent, true)
}

func prep() error {
	if err := registerConfiguration(); err != nil {
		return err
	}

	if err := registerConfigUpdater(); err != nil {
		return err
	}

	if err := registerMigrations(); err != nil {
		return err
	}

	// Setup icon storage location.
	iconsDir := dataroot.Root().ChildDir("databases", 0o0700).ChildDir("icons", 0o0700)
	if err := iconsDir.Ensure(); err != nil {
		return fmt.Errorf("failed to create/check icons directory: %w", err)
	}
	profileIconStoragePath = iconsDir.Path

	return nil
}

func start() error {
	updatesPath = updates.RootPath()
	if updatesPath != "" {
		updatesPath += string(os.PathSeparator)
	}

	if err := loadProfilesMetadata(); err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Warningf("profile: failed to load profiles metadata, falling back to empty state: %s", err)
		}
		meta = &ProfilesMetadata{}
	}
	meta.check()

	if err := migrations.Migrate(module.Ctx); err != nil {
		log.Errorf("profile: migrations failed: %s", err)
	}

	err := registerValidationDBHook()
	if err != nil {
		return err
	}

	err = registerRevisionProvider()
	if err != nil {
		return err
	}

	err = startProfileUpdateChecker()
	if err != nil {
		return err
	}

	module.StartServiceWorker("clean active profiles", 0, cleanActiveProfiles)

	err = updateGlobalConfigProfile(module.Ctx, nil)
	if err != nil {
		log.Warningf("profile: error during loading global profile from configuration: %s", err)
	}

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

func stop() error {
	return meta.Save()
}
