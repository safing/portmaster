package netenv

import (
	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
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
	checkForIPv6Stack()

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

var ipv6Enabled = abool.NewBool(true)

// IPv6Enabled returns whether the device has an active IPv6 stack.
// This is only checked once on startup in order to maintain consistency.
func IPv6Enabled() bool {
	return ipv6Enabled.IsSet()
}

func checkForIPv6Stack() {
	_, v6IPs, err := GetAssignedAddresses()
	if err != nil {
		log.Warningf("netenv: failed to get assigned addresses to check for ipv6 stack: %s", err)
		return
	}

	// Set IPv6 as enabled if any IPv6 addresses are found.
	ipv6Enabled.SetTo(len(v6IPs) > 0)
}
