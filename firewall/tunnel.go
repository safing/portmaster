package firewall

import (
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/resolver"
	"github.com/safing/spn/navigator"
)

func setCustomTunnelOptionsForPortmaster(conn *network.Connection) {
	switch {
	case !tunnelEnabled():
		// Ignore when tunneling is not enabled.
		return
	case !conn.Entity.IPScope.IsGlobal():
		// Ignore if destination is not in global address space.
		return
	case resolver.IsResolverAddress(conn.Entity.IP, conn.Entity.Port):
		// Set custom tunnel options for DNS servers.
		conn.TunnelOpts = &navigator.Options{
			RoutingProfile: navigator.RoutingProfileHomeName,
		}
	}
}
