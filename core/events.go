package core

import (
	"context"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
)

const (
	eventShutdown = "shutdown"
	eventRestart  = "restart"
	restartCode   = 23
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

func shutdown(ctx context.Context, _ interface{}) error {
	log.Warning("core: user requested shutdown")
	go modules.Shutdown() //nolint:errcheck
	return nil
}

func restart(ctx context.Context, data interface{}) error {
	log.Info("core: user requested restart")
	modules.SetExitStatusCode(restartCode)
	go modules.Shutdown() //nolint:errcheck
	return nil
}
