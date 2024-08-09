package captain

import (
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
)

func startDockHooks() {
	docks.RegisterCraneUpdateHook(handleCraneUpdate)
}

func stopDockHooks() {
	docks.ResetCraneUpdateHook()
}

func handleCraneUpdate(crane *docks.Crane) {
	if crane == nil {
		return
	}

	if conf.Client() && crane.Controller != nil && crane.Controller.Abandoning.IsSet() {
		// Check connection to home hub.
		triggerClientHealthCheck()
	}

	if conf.PublicHub() && crane.Public() {
		// Update Hub status.
		updateConnectionStatus()
	}
}

func updateConnectionStatus() {
	// Delay updating status for a better chance to combine multiple changes.
	module.statusUpdater.Delay(maintainStatusUpdateDelay)

	// Check if we lost all connections and trigger a pending restart if we did.
	for _, crane := range docks.GetAllAssignedCranes() {
		if crane.Public() && !crane.Stopped() {
			// There is at least one public and active crane, so don't restart now.
			return
		}
	}
	updates.TriggerRestartIfPending()
}
