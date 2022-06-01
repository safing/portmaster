package profile

import (
	"os"

	"github.com/safing/portbase/database/migration"
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

func init() {
	module = modules.Register("profiles", prep, start, nil, "base", "updates")
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

	return nil
}

func start() error {
	updatesPath = updates.RootPath()
	if updatesPath != "" {
		updatesPath += string(os.PathSeparator)
	}

	if err := migrations.Migrate(module.Ctx); err != nil {
		return err
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

	return nil
}
