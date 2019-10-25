package environment

import (
	"errors"

	"github.com/safing/portbase/modules"
)

const (
	networkChangedEvent      = "network changed"
	onlineStatusChangedEvent = "online status changed"
)

var (
	module *modules.Module
)

func InitSubModule(m *modules.Module) {
	module = m
	module.RegisterEvent(networkChangedEvent)
	module.RegisterEvent(onlineStatusChangedEvent)
}

func StartSubModule() error {
	if module == nil {
		return errors.New("not initialized")
	}

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
