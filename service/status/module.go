package status

import (
	"context"
	"fmt"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/utils/debug"
	"github.com/safing/portmaster/service/netenv"
)

var module *modules.Module

func init() {
	module = modules.Register("status", nil, start, nil, "base", "config")
}

func start() error {
	if err := setupRuntimeProvider(); err != nil {
		return err
	}

	if err := module.RegisterEventHook(
		netenv.ModuleName,
		netenv.OnlineStatusChangedEvent,
		"update online status in system status",
		func(_ context.Context, _ interface{}) error {
			pushSystemStatus()
			return nil
		},
	); err != nil {
		return err
	}
	return nil
}

// AddToDebugInfo adds the system status to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	di.AddSection(
		fmt.Sprintf("Status: %s", netenv.GetOnlineStatus()),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		fmt.Sprintf("OnlineStatus:          %s", netenv.GetOnlineStatus()),
		"CaptivePortal:         "+netenv.GetCaptivePortal().URL,
	)
}
