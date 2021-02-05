package firewall

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/resolver"
)

func filterDNSSection(entries []dns.RR, p *profile.LayeredProfile, scope int8) ([]dns.RR, []string, int, string) {
	goodEntries := make([]dns.RR, 0, len(entries))
	filteredRecords := make([]string, 0, len(entries))

	// keeps track of the number of valid and allowed
	// A and AAAA records.
	var allowedAddressRecords int
	var interveningOptionKey string

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
				interveningOptionKey = profile.CfgOptionRemoveOutOfScopeDNSKey
				continue
			case scope == netutils.Global && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
				// No global DNS should return LAN addresses
				filteredRecords = append(filteredRecords, rr.String())
				interveningOptionKey = profile.CfgOptionRemoveOutOfScopeDNSKey
				continue
			}
		}

		if p.RemoveBlockedDNS() {
			// filter by flags
			switch {
			case p.BlockScopeInternet() && classification == netutils.Global:
				filteredRecords = append(filteredRecords, rr.String())
				interveningOptionKey = profile.CfgOptionBlockScopeInternetKey
				continue
			case p.BlockScopeLAN() && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
				filteredRecords = append(filteredRecords, rr.String())
				interveningOptionKey = profile.CfgOptionBlockScopeLANKey
				continue
			case p.BlockScopeLocal() && classification == netutils.HostLocal:
				filteredRecords = append(filteredRecords, rr.String())
				interveningOptionKey = profile.CfgOptionBlockScopeLocalKey
				continue
			}

			// TODO: filter by endpoint list (IP only)
		}

		// if survived, add to good entries
		allowedAddressRecords++
		goodEntries = append(goodEntries, rr)
	}

	return goodEntries, filteredRecords, allowedAddressRecords, interveningOptionKey
}

func filterDNSResponse(conn *network.Connection, rrCache *resolver.RRCache) *resolver.RRCache {
	p := conn.Process().Profile()

	// do not modify own queries
	if conn.Process().Pid == ownPID {
		return rrCache
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
	var interveningOptionKey string

	rrCache.Answer, filteredRecords, validIPs, interveningOptionKey = filterDNSSection(rrCache.Answer, p, rrCache.ServerScope)
	rrCache.FilteredEntries = append(rrCache.FilteredEntries, filteredRecords...)

	// we don't count the valid IPs in the extra section
	rrCache.Extra, filteredRecords, _, _ = filterDNSSection(rrCache.Extra, p, rrCache.ServerScope)
	rrCache.FilteredEntries = append(rrCache.FilteredEntries, filteredRecords...)

	if len(rrCache.FilteredEntries) > 0 {
		rrCache.Filtered = true
		if validIPs == 0 {
			switch interveningOptionKey {
			case profile.CfgOptionBlockScopeInternetKey:
				conn.Block("Internet access blocked", interveningOptionKey)
			case profile.CfgOptionBlockScopeLANKey:
				conn.Block("LAN access blocked", interveningOptionKey)
			case profile.CfgOptionBlockScopeLocalKey:
				conn.Block("Localhost access blocked", interveningOptionKey)
			case profile.CfgOptionRemoveOutOfScopeDNSKey:
				conn.Block("DNS global/private split-view violation", interveningOptionKey)
			default:
				conn.Block("DNS response only contained to-be-blocked IPs", interveningOptionKey)
			}

			// If all entries are filtered, this could mean that these are broken/bogus resource records.
			if rrCache.Expired() {
				// If the entry is expired, force delete it.
				err := resolver.ResetCachedRecord(rrCache.Domain, rrCache.Question.String())
				if err != nil && err != database.ErrNotFound {
					log.Warningf(
						"filter: failed to delete fully filtered name cache for %s: %s",
						rrCache.ID(),
						err,
					)
				}
			} else if rrCache.Expires > time.Now().Add(10*time.Second).Unix() {
				// Set a low TTL of 10 seconds if TTL is higher than that.
				rrCache.Expires = time.Now().Add(10 * time.Second).Unix()
				err := rrCache.Save()
				if err != nil {
					log.Debugf(
						"filter: failed to set shorter TTL on fully filtered name cache for %s: %s",
						rrCache.ID(),
						err,
					)
				}
			}

			return nil
		}

		log.Infof("filter: filtered DNS replies for %s: %s", conn, strings.Join(rrCache.FilteredEntries, ", "))
	}

	return rrCache
}

// DecideOnResolvedDNS filters a dns response according to the application profile and settings.
func DecideOnResolvedDNS(
	ctx context.Context,
	conn *network.Connection,
	q *resolver.Query,
	rrCache *resolver.RRCache,
) *resolver.RRCache {

	// special grant for connectivity domains
	if checkConnectivityDomain(ctx, conn, nil) {
		// returns true if check triggered
		return rrCache
	}

	updatedRR := filterDNSResponse(conn, rrCache)
	if updatedRR == nil {
		return nil
	}

	updateIPsAndCNAMEs(q, rrCache, conn)

	if mayBlockCNAMEs(ctx, conn) {
		return nil
	}

	return updatedRR
}

func mayBlockCNAMEs(ctx context.Context, conn *network.Connection) bool {
	// if we have CNAMEs and the profile is configured to filter them
	// we need to re-check the lists and endpoints here
	if conn.Process().Profile().FilterCNAMEs() {
		conn.Entity.ResetLists()
		conn.Entity.EnableCNAMECheck(ctx, true)

		result, reason := conn.Process().Profile().MatchEndpoint(ctx, conn.Entity)
		if result == endpoints.Denied {
			conn.BlockWithContext(reason.String(), profile.CfgOptionFilterCNAMEKey, reason.Context())
			return true
		}

		if result == endpoints.NoMatch {
			result, reason = conn.Process().Profile().MatchFilterLists(ctx, conn.Entity)
			if result == endpoints.Denied {
				conn.BlockWithContext(reason.String(), profile.CfgOptionFilterCNAMEKey, reason.Context())
				return true
			}
		}
	}

	return false
}

// updateIPsAndCNAMEs saves all the IP->Name mappings to the cache database and
// updates the CNAMEs in the Connection's Entity.
func updateIPsAndCNAMEs(q *resolver.Query, rrCache *resolver.RRCache, conn *network.Connection) {
	// Get profileID for scoping IPInfo.
	var profileID string
	proc := conn.Process()
	if proc != nil {
		profileID = proc.LocalProfileKey
	}

	// Collect IPs and CNAMEs.
	cnames := make(map[string]string)
	ips := make([]net.IP, 0, len(rrCache.Answer))

	for _, rr := range append(rrCache.Answer, rrCache.Extra...) {
		switch v := rr.(type) {
		case *dns.CNAME:
			cnames[v.Hdr.Name] = v.Target

		case *dns.A:
			ips = append(ips, v.A)

		case *dns.AAAA:
			ips = append(ips, v.AAAA)
		}
	}

	// Package IPs and CNAMEs into IPInfo structs.
	for _, ip := range ips {
		// Never save domain attributions for localhost IPs.
		if netutils.ClassifyIP(ip) == netutils.HostLocal {
			continue
		}

		// Create new record for this IP.
		record := resolver.ResolvedDomain{
			Domain:  q.FQDN,
			Expires: rrCache.Expires,
		}

		// Resolve all CNAMEs in the correct order and add the to the record.
		var domain = q.FQDN
		for {
			nextDomain, isCNAME := cnames[domain]
			if !isCNAME {
				break
			}

			record.CNAMEs = append(record.CNAMEs, nextDomain)
			domain = nextDomain
		}

		// Update the entity to include the CNAMEs of the query response.
		conn.Entity.CNAME = record.CNAMEs

		// Check if there is an existing record for this DNS response.
		// Else create a new one.
		ipString := ip.String()
		info, err := resolver.GetIPInfo(profileID, ipString)
		if err != nil {
			if err != database.ErrNotFound {
				log.Errorf("nameserver: failed to search for IP info record: %s", err)
			}

			info = &resolver.IPInfo{
				IP:        ipString,
				ProfileID: profileID,
			}
		}

		// Add the new record to the resolved domains for this IP and scope.
		info.AddDomain(record)

		// Save if the record is new or has been updated.
		if err := info.Save(); err != nil {
			log.Errorf("nameserver: failed to save IP info record: %s", err)
		}
	}
}
