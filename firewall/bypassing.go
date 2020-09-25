package firewall

import (
	"strings"

	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/profile/endpoints"
)

// PreventBypassing checks if the connection should be denied or permitted
// based on some bypass protection checks.
func PreventBypassing(conn *network.Connection) (endpoints.EPResult, string, nsutil.Responder) {
	// Block firefox canary domain to disable DoH
	if strings.ToLower(conn.Entity.Domain) == "use-application-dns.net." {
		return endpoints.Denied,
			"blocked canary domain to prevent enabling of DNS-over-HTTPs",
			nsutil.NxDomain()
	}

	return endpoints.NoMatch, "", nil
}
