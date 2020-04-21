package firewall

import (
	"net"
	"os"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/resolver"
)

func filterDNSSection(entries []dns.RR, p *profile.LayeredProfile, scope int8) ([]dns.RR, []string, int) {
	goodEntries := make([]dns.RR, 0, len(entries))
	filteredRecords := make([]string, 0, len(entries))

	// keeps track of the number of valid and allowed
	// A and AAAA records.
	var allowedAddressRecords int

	for _, rr := range entries {
		// get IP and classification
		var ip net.IP
		switch v := rr.(type) {
		case *dns.A:
			ip = v.A
		case *dns.AAAA:
			ip = v.AAAA
		default:
			// add non A/AAAA entries
			goodEntries = append(goodEntries, rr)
			continue
		}
		classification := netutils.ClassifyIP(ip)

		if p.RemoveOutOfScopeDNS() {
			switch {
			case classification == netutils.HostLocal:
				// No DNS should return localhost addresses
				filteredRecords = append(filteredRecords, rr.String())
				continue
			case scope == netutils.Global && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
				// No global DNS should return LAN addresses
				filteredRecords = append(filteredRecords, rr.String())
				continue
			}
		}

		if p.RemoveBlockedDNS() {
			// filter by flags
			switch {
			case p.BlockScopeInternet() && classification == netutils.Global:
				filteredRecords = append(filteredRecords, rr.String())
				continue
			case p.BlockScopeLAN() && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
				filteredRecords = append(filteredRecords, rr.String())
				continue
			case p.BlockScopeLocal() && classification == netutils.HostLocal:
				filteredRecords = append(filteredRecords, rr.String())
				continue
			}

			// TODO: filter by endpoint list (IP only)
		}

		// if survived, add to good entries
		allowedAddressRecords++
		goodEntries = append(goodEntries, rr)
	}

	return goodEntries, filteredRecords, allowedAddressRecords
}

func filterDNSResponse(conn *network.Connection, rrCache *resolver.RRCache) *resolver.RRCache {
	// do not modify own queries
	if conn.Process().Pid == os.Getpid() {
		return rrCache
	}

	// get profile
	p := conn.Process().Profile()
	if p == nil {
		conn.Block("no profile")
		return nil
	}

	// check if DNS response filtering is completely turned off
	if !p.RemoveOutOfScopeDNS() && !p.RemoveBlockedDNS() {
		return rrCache
	}

	// duplicate entry
	rrCache = rrCache.ShallowCopy()
	rrCache.FilteredEntries = make([]string, 0)

	var filteredRecords []string
	var validIPs int

	rrCache.Answer, filteredRecords, validIPs = filterDNSSection(rrCache.Answer, p, rrCache.ServerScope)
	rrCache.FilteredEntries = append(rrCache.FilteredEntries, filteredRecords...)

	// FIXME(ppacher): should we consider valid IPs from the extra section?
	rrCache.Extra, filteredRecords, _ = filterDNSSection(rrCache.Extra, p, rrCache.ServerScope)
	rrCache.FilteredEntries = append(rrCache.FilteredEntries, filteredRecords...)

	if len(rrCache.FilteredEntries) > 0 {
		rrCache.Filtered = true
		if validIPs == 0 {
			conn.Block("no addresses returned for this domain are permitted")
			return nil
		}

		log.Infof("filter: filtered DNS replies for %s: %s", conn, strings.Join(rrCache.FilteredEntries, ", "))
	}

	return rrCache
}

// DecideOnResolvedDNS filters a dns response according to the application profile and settings.
func DecideOnResolvedDNS(conn *network.Connection, q *resolver.Query, rrCache *resolver.RRCache) *resolver.RRCache {
	updatedRR := filterDNSResponse(conn, rrCache)
	if updatedRR == nil {
		return nil
	}

	updateIPsAndCNAMEs(q, rrCache, conn)

	if mayBlockCNAMEs(conn) {
		return nil
	}

	// TODO: Gate17 integration
	// tunnelInfo, err := AssignTunnelIP(fqdn)

	return updatedRR
}

func mayBlockCNAMEs(conn *network.Connection) bool {
	// if we have CNAMEs and the profile is configured to filter them
	// we need to re-check the lists and endpoints here
	if conn.Process().Profile().FilterCNAMEs() {
		conn.Entity.ResetLists()
		conn.Entity.EnableCNAMECheck(true)

		result, reason := conn.Process().Profile().MatchEndpoint(conn.Entity)
		if result == endpoints.Denied {
			conn.BlockWithContext(reason.String(), reason.Context())
			return true
		}

		if result == endpoints.NoMatch {
			result, reason = conn.Process().Profile().MatchFilterLists(conn.Entity)
			if result == endpoints.Denied {
				conn.BlockWithContext(reason.String(), reason.Context())
				return true
			}
		}
	}

	return false
}

func updateIPsAndCNAMEs(q *resolver.Query, rrCache *resolver.RRCache, conn *network.Connection) {
	// save IP addresses to IPInfo
	cnames := make(map[string]string)
	ips := make(map[string]struct{})

	for _, rr := range append(rrCache.Answer, rrCache.Extra...) {
		switch v := rr.(type) {
		case *dns.CNAME:
			cnames[v.Hdr.Name] = v.Target

		case *dns.A:
			ips[v.A.String()] = struct{}{}

		case *dns.AAAA:
			ips[v.AAAA.String()] = struct{}{}
		}
	}

	for ip := range ips {
		record := resolver.ResolvedDomain{
			Domain: q.FQDN,
		}

		// resolve all CNAMEs in the correct order.
		var domain = q.FQDN
		for {
			nextDomain, isCNAME := cnames[domain]
			if !isCNAME {
				break
			}

			record.CNAMEs = append(record.CNAMEs, nextDomain)
			domain = nextDomain
		}

		// update the entity to include the cnames
		conn.Entity.CNAME = record.CNAMEs

		// get the existing IP info or create a new  one
		var save bool
		info, err := resolver.GetIPInfo(ip)
		if err != nil {
			if err != database.ErrNotFound {
				log.Errorf("nameserver: failed to search for IP info record: %s", err)
			}

			info = &resolver.IPInfo{
				IP: ip,
			}
			save = true
		}

		// and the new resolved domain record and save
		if new := info.AddDomain(record); new {
			save = true
		}
		if save {
			if err := info.Save(); err != nil {
				log.Errorf("nameserver: failed to save IP info record: %s", err)
			}
		}
	}
}
