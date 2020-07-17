package status

import (
	"context"

	"github.com/safing/portmaster/netenv"
)

// startNetEnvHooking starts the listener for online status changes.
func startNetEnvHooking() error {
	return module.RegisterEventHook(
		"netenv",
		netenv.OnlineStatusChangedEvent,
		"update online status in system status",
		func(_ context.Context, _ interface{}) error {
			status.Lock()
			status.updateOnlineStatus()
			status.Unlock()
			status.Save()
			return nil
		},
	)
}

func (s *SystemStatus) updateOnlineStatus() {
	s.OnlineStatus = netenv.GetOnlineStatus()
	s.CaptivePortal = netenv.GetCaptivePortal()
}
