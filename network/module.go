package network

import (
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/state"
)

var (
	module *modules.Module

	defaultFirewallHandler FirewallHandler
)

func init() {
	module = modules.Register("network", prep, start, nil, "base", "netenv", "processes")
}

// SetDefaultFirewallHandler sets the default firewall handler.
func SetDefaultFirewallHandler(handler FirewallHandler) {
	if defaultFirewallHandler == nil {
		defaultFirewallHandler = handler
	}
}

func prep() error {
	if netenv.IPv6Enabled() {
		state.EnableTCPDualStack()
		state.EnableUDPDualStack()
	}

	return registerAPIEndpoints()
}

func start() error {
	err := registerAsDatabase()
	if err != nil {
		return err
	}

	if err := registerMetrics(); err != nil {
		return err
	}

	module.StartServiceWorker("clean connections", 0, connectionCleaner)
	module.StartServiceWorker("write open dns requests", 0, openDNSRequestWriter)

	return nil
}
