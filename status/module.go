package status

import (
	"context"
	"fmt"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/utils/debug"
	"github.com/safing/portmaster/netenv"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("status", nil, start, nil, "base")
}

func start() error {
	if err := setupRuntimeProvider(); err != nil {
		return err
	}

	module.StartWorker("auto-pilot", autoPilot)

	triggerAutopilot()

	err := module.RegisterEventHook(
		"netenv",
		netenv.OnlineStatusChangedEvent,
		"update online status in system status",
		func(_ context.Context, _ interface{}) error {
			triggerAutopilot()
			return nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}

// AddToDebugInfo adds the system status to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	di.AddSection(
		fmt.Sprintf("Status: %s", SecurityLevelString(ActiveSecurityLevel())),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		fmt.Sprintf("ActiveSecurityLevel:   %s", SecurityLevelString(ActiveSecurityLevel())),
		fmt.Sprintf("SelectedSecurityLevel: %s", SecurityLevelString(SelectedSecurityLevel())),
		fmt.Sprintf("ThreatMitigationLevel: %s", SecurityLevelString(getHighestMitigationLevel())),
		fmt.Sprintf("CaptivePortal:         %s", netenv.GetCaptivePortal().URL),
		fmt.Sprintf("OnlineStatus:          %s", netenv.GetOnlineStatus()),
	)
}
