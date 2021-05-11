package core

import (
	"context"
	"net/http"
	"time"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/utils/debug"
	"github.com/safing/portmaster/status"
	"github.com/safing/portmaster/updates"
)

const (
	eventShutdown = "shutdown"
	eventRestart  = "restart"
)

func registerEvents() {
	module.RegisterEvent(eventShutdown, true)
	module.RegisterEvent(eventRestart, true)
}

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       "core/shutdown",
		Write:      api.PermitSelf,
		ActionFunc: shutdown,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       "core/restart",
		Write:      api.PermitAdmin,
		ActionFunc: restart,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "debug/core",
		Read:        api.PermitAnyone,
		DataFunc:    debugInfo,
		Name:        "Get Debug Information",
		Description: "Returns network debugging information, similar to debug/info, but with system status data.",
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "style",
			Value:       "github",
			Description: "Specify the formatting style. The default is simple markdown formatting.",
		}},
	}); err != nil {
		return err
	}

	return nil
}

// shutdown shuts the Portmaster down.
func shutdown(_ *api.Request) (msg string, err error) {
	log.Warning("core: user requested shutdown via action")

	module.StartWorker("shutdown", func(context.Context) error {
		// Notify everyone of the shutdown.
		module.TriggerEvent(eventShutdown, nil)
		// Wait a bit for the event to propagate.
		time.Sleep(1 * time.Second)

		// Do not run in worker, as this would block itself here.
		go modules.Shutdown() //nolint:errcheck
		return nil
	})

	return "shutdown initiated", nil
}

// restart restarts the Portmaster.
func restart(_ *api.Request) (msg string, err error) {
	log.Info("core: user requested restart via action")

	module.StartWorker("restart", func(context.Context) error {
		// Notify everyone of the shutdown.
		module.TriggerEvent(eventRestart, nil)
		// Wait a bit for the event to propagate.
		time.Sleep(1 * time.Second)

		updates.RestartNow()
		return nil
	})

	return "restart initiated", nil
}

// debugInfo returns the debugging information for support requests.
func debugInfo(ar *api.Request) (data []byte, err error) {
	// Create debug information helper.
	di := new(debug.Info)
	di.Style = ar.Request.URL.Query().Get("style")

	// Add debug information.
	di.AddVersionInfo()
	di.AddPlatformInfo(ar.Context())
	status.AddToDebugInfo(di)
	di.AddLastReportedModuleError()
	di.AddLastUnexpectedLogs()
	di.AddGoroutineStack()

	// Return data.
	return di.Bytes(), nil
}
