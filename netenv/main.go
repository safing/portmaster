package netenv

import (
	"github.com/safing/portbase/modules"
)

const (
	networkChangedEvent      = "network changed"
	onlineStatusChangedEvent = "online status changed"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("netenv", nil, start, nil)
	module.RegisterEvent(networkChangedEvent)
	module.RegisterEvent(onlineStatusChangedEvent)
}

func start() error {
	module.StartServiceWorker(
		"monitor network changes",
		0,
		monitorNetworkChanges,
	)

	module.StartServiceWorker(
		"monitor online status",
		0,
		monitorOnlineStatus,
	)

	return nil
}
