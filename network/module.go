package network

import (
	"net"

	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module

	dnsAddress        = net.IPv4(127, 0, 0, 1)
	dnsPort    uint16 = 53

	defaultFirewallHandler FirewallHandler
)

func init() {
	module = modules.Register("network", nil, start, nil, "base", "processes")
}

// SetDefaultFirewallHandler sets the default firewall handler.
func SetDefaultFirewallHandler(handler FirewallHandler) {
	if defaultFirewallHandler == nil {
		defaultFirewallHandler = handler
	}
}

func start() error {
	err := registerAsDatabase()
	if err != nil {
		return err
	}

	module.StartServiceWorker("clean connections", 0, connectionCleaner)
	module.StartServiceWorker("write open dns requests", 0, openDNSRequestWriter)

	return nil
}
