package firewall

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/service/resolver"
)

func filterDNSSection(
	ctx context.Context,
	entries []dns.RR,
	p *profile.LayeredProfile,
	resolverScope netutils.IPScope,
	sysResolver bool,
) ([]dns.RR, []string, int, string) {
	// Will be filled 1:1 most of the time.
	goodEntries := make([]dns.RR, 0, len(entries))

	// Will stay empty most of the time.
	var filteredRecords []string

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
			// TODO: Add support for dns.SVCB and dns.HTTPS
			goodEntries = append(goodEntries, rr)
			continue
		}
		ipScope := netutils.GetIPScope(ip)

		if p.RemoveOutOfScopeDNS() {
			switch {
			case ipScope.IsLocalhost():
				// No DNS should return localhost addresses
				filteredRecords = append(filteredRecords, formatRR(rr))
				interveningOptionKey = profile.CfgOptionRemoveOutOfScopeDNSKey
				log.Tracer(ctx).Tracef("filter: RR violates resolver scope: %s", formatRR(rr))
				continue

			case resolverScope.IsGlobal() && ipScope.IsLAN() && !sysResolver:
				// No global DNS should return LAN addresses
				filteredRecords = append(filteredRecords, formatRR(rr))
				interveningOptionKey = profile.CfgOptionRemoveOutOfScopeDNSKey
				log.Tracer(ctx).Tracef("filter: RR violates resolver scope: %s", formatRR(rr))
				continue
			}
		}

		if p.RemoveBlockedDNS() && !sysResolver {
			// filter by flags
			switch {
			case p.BlockScopeInternet() && ipScope.IsGlobal():
				filteredRecords = append(filteredRecords, formatRR(rr))
				interveningOptionKey = profile.CfgOptionBlockScopeInternetKey
				log.Tracer(ctx).Tracef("filter: RR is in blocked scope Internet: %s", formatRR(rr))
				continue

			case p.BlockScopeLAN() && ipScope.IsLAN():
				filteredRecords = append(filteredRecords, formatRR(rr))
				interveningOptionKey = profile.CfgOptionBlockScopeLANKey
				log.Tracer(ctx).Tracef("filter: RR is in blocked scope LAN: %s", formatRR(rr))
				continue

			case p.BlockScopeLocal() && ipScope.IsLocalhost():
				filteredRecords = append(filteredRecords, formatRR(rr))
				interveningOptionKey = profile.CfgOptionBlockScopeLocalKey
				log.Tracer(ctx).Tracef("filter: RR is in blocked scope Localhost: %s", formatRR(rr))
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

func filterDNSResponse(
	ctx context.Context,
	conn *network.Connection,
	p *profile.LayeredProfile,
	rrCache *resolver.RRCache,
	sysResolver bool,
) *resolver.RRCache {
	// do not modify own queries
	if conn.Process().Pid == ownPID {
		return rrCache
	}

	// check if DNS response filtering is completely turned off
	if !p.RemoveOutOfScopeDNS() && !p.RemoveBlockedDNS() {
		return rrCache
	}

	var filteredRecords []string
	var validIPs int
	var interveningOptionKey string

	rrCache.Answer, filteredRecords, validIPs, interveningOptionKey = filterDNSSection(ctx, rrCache.Answer, p, rrCache.Resolver.IPScope, sysResolver)
	if len(filteredRecords) > 0 {
		rrCache.FilteredEntries = append(rrCache.FilteredEntries, filteredRecords...)
	}

	// Don't count the valid IPs in the extra section.
	rrCache.Extra, filteredRecords, _, _ = filterDNSSection(ctx, rrCache.Extra, p, rrCache.Resolver.IPScope, sysResolver)
	if len(filteredRecords) > 0 {
		rrCache.FilteredEntries = append(rrCache.FilteredEntries, filteredRecords...)
	}

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

			return rrCache
		}
	}

	return rrCache
}

// FilterResolvedDNS filters a dns response according to the application
// profile and settings.
func FilterResolvedDNS(
	ctx context.Context,
	conn *network.Connection,
	q *resolver.Query,
	rrCache *resolver.RRCache,
) *resolver.RRCache {
	// Check if we have a process and profile.
	layeredProfile := conn.Process().Profile()
	if layeredProfile == nil {
		log.Tracer(ctx).Warning("unknown process or profile")
		return nil
	}

	// Don't filter env responses.
	if rrCache.Resolver.Type == resolver.ServerTypeEnv {
		return rrCache
	}

	// special grant for connectivity domains
	if checkConnectivityDomain(ctx, conn, layeredProfile, nil) {
		// returns true if check triggered
		return rrCache
	}

	// Only filter critical things if request comes from the system resolver.
	sysResolver := conn.Process().IsSystemResolver()

	// Filter dns records and return if the query is blocked.
	rrCache = filterDNSResponse(ctx, conn, layeredProfile, rrCache, sysResolver)
	if conn.Verdict == network.VerdictBlock {
		return rrCache
	}

	// Block by CNAMEs.
	if !sysResolver {
		mayBlockCNAMEs(ctx, conn, layeredProfile)
	}

	return rrCache
}

func mayBlockCNAMEs(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile) bool {
	// if we have CNAMEs and the profile is configured to filter them
	// we need to re-check the lists and endpoints here
	if p.FilterCNAMEs() {
		conn.Entity.ResetLists()
		conn.Entity.EnableCNAMECheck(ctx, true)

		result, reason := p.MatchEndpoint(ctx, conn.Entity)
		if result == endpoints.Denied {
			conn.BlockWithContext(reason.String(), profile.CfgOptionFilterCNAMEKey, reason.Context())
			return true
		}

		if result == endpoints.NoMatch {
			result, reason = p.MatchFilterLists(ctx, conn.Entity)
			if result == endpoints.Denied {
				conn.BlockWithContext(reason.String(), profile.CfgOptionFilterCNAMEKey, reason.Context())
				return true
			}
		}
	}

	return false
}

// UpdateIPsAndCNAMEs saves all the IP->Name mappings to the cache database and
// updates the CNAMEs in the Connection's Entity.
func UpdateIPsAndCNAMEs(q *resolver.Query, rrCache *resolver.RRCache, conn *network.Connection) {
	// Sanity check input, as this is called from defer.
	if q == nil || rrCache == nil {
		return
	}

	// Get profileID for scoping IPInfo.
	var profileID string
	localProfile := conn.Process().Profile().LocalProfile()
	switch localProfile.ID {
	case profile.UnidentifiedProfileID,
		profile.SystemResolverProfileID:
		profileID = resolver.IPInfoProfileScopeGlobal
	default:
		profileID = localProfile.ID
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

		case *dns.SVCB:
			if len(v.Target) >= 2 { // Ignore "" and ".".
				cnames[v.Hdr.Name] = v.Target
			}
			for _, pair := range v.Value {
				switch svcbParam := pair.(type) {
				case *dns.SVCBIPv4Hint:
					ips = append(ips, svcbParam.Hint...)
				case *dns.SVCBIPv6Hint:
					ips = append(ips, svcbParam.Hint...)
				}
			}

		case *dns.HTTPS:
			if len(v.Target) >= 2 { // Ignore "" and ".".
				cnames[v.Hdr.Name] = v.Target
			}
			for _, pair := range v.Value {
				switch svcbParam := pair.(type) {
				case *dns.SVCBIPv4Hint:
					ips = append(ips, svcbParam.Hint...)
				case *dns.SVCBIPv6Hint:
					ips = append(ips, svcbParam.Hint...)
				}
			}
		}
	}

	// Create new record for this IP.
	record := resolver.ResolvedDomain{
		Domain:            q.FQDN,
		Resolver:          rrCache.Resolver,
		DNSRequestContext: rrCache.ToDNSRequestContext(),
		Expires:           rrCache.Expires,
	}
	// Process CNAMEs
	record.AddCNAMEs(cnames)
	// Link connection with cnames.
	if conn.Type == network.DNSRequest {
		conn.Entity.CNAME = record.CNAMEs
	}

	SaveIPsInCache(ips, profileID, record)
}

// formatRR is a friendlier alternative to miekg/dns.RR.String().
func formatRR(rr dns.RR) string {
	return strings.ReplaceAll(rr.String(), "\t", " ")
}

// SaveIPsInCache saves the provided ips in the dns cashe assoseted with the record Domain and CNAMEs.
func SaveIPsInCache(ips []net.IP, profileID string, record resolver.ResolvedDomain) {
	// Package IPs and CNAMEs into IPInfo structs.
	for _, ip := range ips {
		// Never save domain attributions for localhost IPs.
		if netutils.GetIPScope(ip) == netutils.HostLocal {
			continue
		}

		ipString := ip.String()
		info, err := resolver.GetIPInfo(profileID, ipString)
		if err != nil {
			if !errors.Is(err, database.ErrNotFound) {
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
