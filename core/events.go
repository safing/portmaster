// DEPRECATED: remove in v0.7

package core

import (
	"context"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

const (
	eventShutdown = "shutdown"
	eventRestart  = "restart"
)

func registerEvents() {
	module.RegisterEvent(eventShutdown)
	module.RegisterEvent(eventRestart)
}

func registerEventHooks() error {
	err := module.RegisterEventHook("core", eventShutdown, "execute shutdown", shutdownHook)
	if err != nil {
		return err
	}

	err = module.RegisterEventHook("core", eventRestart, "execute shutdown", restartHook)
	if err != nil {
		return err
	}

	return nil
}

// shutdownHook shuts the Portmaster down.
func shutdownHook(ctx context.Context, _ interface{}) error {
	log.Warning("core: user requested shutdown")
	// Do not use a worker, as this would block itself here.
	go modules.Shutdown() //nolint:errcheck
	return nil
}

// restartHook restarts the Portmaster.
func restartHook(ctx context.Context, data interface{}) error {
	log.Info("core: user requested restart")
	updates.RestartNow()
	return nil
}
