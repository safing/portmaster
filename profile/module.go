package profile

import (
	"github.com/safing/portbase/log"

	"github.com/safing/portbase/modules"

	// module dependencies
	_ "github.com/safing/portmaster/core/base"
	_ "github.com/safing/portmaster/updates" // dependency of semi-dependency filterlists
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("profiles", prep, start, nil, "base", "updates")
}

func prep() error {
	err := registerConfiguration()
	if err != nil {
		return err
	}

	err = registerConfigUpdater()
	if err != nil {
		return err
	}

	return nil
}

func start() error {
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
