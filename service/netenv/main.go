package netenv

import (
	"errors"
	"sync/atomic"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
)

// Event Names.
const (
	ModuleName               = "netenv"
	NetworkChangedEvent      = "network changed"
	OnlineStatusChangedEvent = "online status changed"
)

type NetEnv struct {
	instance instance

	EventNetworkChange      *mgr.EventMgr[struct{}]
	EventOnlineStatusChange *mgr.EventMgr[OnlineStatus]
}

func (ne *NetEnv) Start(m *mgr.Manager) error {
	if err := prep(); err != nil {
		return err
	}

	m.Go(
		"monitor network changes",
		monitorNetworkChanges,
	)

	m.Go(
		"monitor online status",
		monitorOnlineStatus,
	)

	return nil
}

func (ne *NetEnv) Stop(m *mgr.Manager) error {
	return nil
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

var (
	module     *NetEnv
	shimLoaded atomic.Bool
)

// New returns a new NetEnv module.
func New(instance instance) (*NetEnv, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	if err := prep(); err != nil {
		return nil, err
	}

	module = &NetEnv{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
