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
	err := module.RegisterEventHook("core", eventShutdown, "execute shutdown", shutdown)
	if err != nil {
		return err
	}

	err = module.RegisterEventHook("core", eventRestart, "execute shutdown", restart)
	if err != nil {
		return err
	}

	return nil
}

// shutdown shuts the Portmaster down.
func shutdown(ctx context.Context, _ interface{}) error {
	log.Warning("core: user requested shutdown")
	// Do not use a worker, as this would block itself here.
	go modules.Shutdown() //nolint:errcheck
	return nil
}

// restart restarts the Portmaster.
func restart(ctx context.Context, data interface{}) error {
	log.Info("core: user requested restart")
	modules.SetExitStatusCode(updates.RestartExitCode)
	// Do not use a worker, as this would block itself here.
	go modules.Shutdown() //nolint:errcheck
	return nil
}
