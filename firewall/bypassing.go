package firewall

import (
	"context"
	"strings"

	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/profile/endpoints"
)

var (
	resolverFilterLists = []string{"17-DNS"}
)

// PreventBypassing checks if the connection should be denied or permitted
// based on some bypass protection checks.
func PreventBypassing(ctx context.Context, conn *network.Connection) (endpoints.EPResult, string, nsutil.Responder) {
	// Block firefox canary domain to disable DoH
	if strings.ToLower(conn.Entity.Domain) == "use-application-dns.net." {
		return endpoints.Denied,
			"blocked canary domain to prevent enabling of DNS-over-HTTPs",
			nsutil.NxDomain()
	}

	if conn.Entity.MatchLists(resolverFilterLists) {
		return endpoints.Denied,
			"blocked rogue connection to DNS resolver",
			nsutil.ZeroIP()
	}

	return endpoints.NoMatch, "", nil
}
