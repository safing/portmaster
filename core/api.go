package core

import (
	"github.com/safing/portbase/api"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

func registerActions() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       "core/shutdown",
		Read:       api.PermitSelf,
		ActionFunc: shutdown,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       "core/restart",
		Read:       api.PermitAdmin,
		ActionFunc: restart,
	}); err != nil {
		return err
	}

	return nil
}

// shutdown shuts the Portmaster down.
func shutdown(_ *api.Request) (msg string, err error) {
	log.Warning("core: user requested shutdown via action")
	// Do not use a worker, as this would block itself here.
	go modules.Shutdown() //nolint:errcheck
	return "shutdown initiated", nil
}

// restart restarts the Portmaster.
func restart(_ *api.Request) (msg string, err error) {
	log.Info("core: user requested restart via action")
	updates.RestartNow()
	return "restart initiated", nil
}
