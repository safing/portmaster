package firewall

import (
	"context"
	"strings"

	"github.com/safing/portmaster/compat"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/profile/endpoints"
)

var resolverFilterLists = []string{"17-DNS"}

// PreventBypassing checks if the connection should be denied or permitted
// based on some bypass protection checks.
func PreventBypassing(ctx context.Context, conn *network.Connection) (endpoints.EPResult, string, nsutil.Responder) {
	// Block firefox canary domain to disable DoH.
	if strings.ToLower(conn.Entity.Domain) == "use-application-dns.net." {
		return endpoints.Denied,
			"blocked canary domain to prevent enabling of DNS-over-HTTPs",
			nsutil.NxDomain()
	}

	// Block direct connections to known DNS resolvers.
	switch packet.IPProtocol(conn.Entity.Protocol) { //nolint:exhaustive // Checking for specific values only.
	case packet.ICMP, packet.ICMPv6:
		// Make an exception for ICMP, as these IPs are also often used for debugging.
	default:
		if conn.Entity.MatchLists(resolverFilterLists) {
			compat.ReportSecureDNSBypassIssue(conn.Process())
			return endpoints.Denied,
				"blocked rogue connection to DNS resolver",
				nsutil.BlockIP()
		}
	}

	return endpoints.NoMatch, "", nil
}
