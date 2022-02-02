package netenv

import (
	"github.com/safing/portbase/modules"
)

// Event Names.
const (
	ModuleName               = "netenv"
	NetworkChangedEvent      = "network changed"
	OnlineStatusChangedEvent = "online status changed"
)

var module *modules.Module

func init() {
	module = modules.Register(ModuleName, prep, start, nil)
	module.RegisterEvent(NetworkChangedEvent, true)
	module.RegisterEvent(OnlineStatusChangedEvent, true)
}

func prep() error {
	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	if err := prepOnlineStatus(); err != nil {
		return err
	}

	return prepLocation()
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
