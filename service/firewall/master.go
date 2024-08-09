package firewall

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/agext/levenshtein"
	"golang.org/x/net/publicsuffix"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/detection/dga"
	"github.com/safing/portmaster/service/intel/customlists"
	"github.com/safing/portmaster/service/intel/filterlists"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
)

const noReasonOptionKey = ""

type deciderFn func(context.Context, *network.Connection, *profile.LayeredProfile, packet.Packet) bool

var defaultDeciders = []deciderFn{
	checkPortmasterConnection,
	checkIfBroadcastReply,
	checkConnectionType,
	checkConnectionScope,
	checkEndpointLists,
	checkInvalidIP,
	checkResolverScope,
	checkConnectivityDomain,
	checkBypassPrevention,
	checkFilterLists,
	checkCustomFilterList,
	checkDomainHeuristics,
	checkAutoPermitRelated,
}

// decideOnConnection makes a decision about a connection.
// When called, the connection and profile is already locked.
func decideOnConnection(ctx context.Context, conn *network.Connection, pkt packet.Packet) {
	// Check if we have a process and profile.
	layeredProfile := conn.Process().Profile()
	if layeredProfile == nil {
		conn.Deny("unknown process or profile", noReasonOptionKey)
		return
	}

	// Check if the layered profile needs updating.
	if layeredProfile.NeedsUpdate() {
		// Update revision counter in connection.
		conn.ProfileRevisionCounter = layeredProfile.Update(
			conn.Process().MatchingData(),
			conn.Process().CreateProfileCallback,
		)
		conn.SaveWhenFinished()

		// Reset verdict for connection.
		log.Tracer(ctx).Infof("filter: profile updated, re-evaluating verdict of %s", conn)

		// Reset entity if it exists.
		if conn.Entity != nil {
			conn.Entity.ResetLists()
		}
	} else {
		// Check if the revision counter of the connection needs updating.
		revCnt := layeredProfile.RevisionCnt()
		if conn.ProfileRevisionCounter != revCnt {
			conn.ProfileRevisionCounter = revCnt
			conn.SaveWhenFinished()
		}
	}

	// prepare the entity and resolve all filterlist matches
	conn.Entity.ResolveSubDomainLists(ctx, layeredProfile.FilterSubDomains())
	conn.Entity.EnableCNAMECheck(ctx, layeredProfile.FilterCNAMEs())
	conn.Entity.LoadLists(ctx)

	// Run all deciders and return if they came to a conclusion.
	done, defaultAction := runDeciders(ctx, defaultDeciders, conn, layeredProfile, pkt)
	if done {
		return
	}

	// DNS Request are always default allowed, as the endpoint lists could not
	// be checked fully.
	if conn.Type == network.DNSRequest {
		conn.Accept("allowing dns request", noReasonOptionKey)
		return
	}

	// Deciders did not conclude, use default action.
	switch defaultAction {
	case profile.DefaultActionPermit:
		conn.Accept("allowed by default action", profile.CfgOptionDefaultActionKey)
	case profile.DefaultActionAsk:
		// Only prompt if there has not been a decision already.
		// This prevents prompts from being created when re-evaluating connections.
		if conn.Verdict == network.VerdictUndecided {
			prompt(ctx, conn)
		}
	default:
		conn.Deny("blocked by default action", profile.CfgOptionDefaultActionKey)
	}
}

func runDeciders(ctx context.Context, selectedDeciders []deciderFn, conn *network.Connection, layeredProfile *profile.LayeredProfile, pkt packet.Packet) (done bool, defaultAction uint8) {
	// Read-lock all the profiles.
	layeredProfile.LockForUsage()
	defer layeredProfile.UnlockForUsage()

	// Go though all deciders, return if one sets an action.
	for _, decider := range selectedDeciders {
		if decider(ctx, conn, layeredProfile, pkt) {
			return true, profile.DefaultActionNotSet
		}
	}

	// Return the default action.
	return false, layeredProfile.DefaultAction()
}

// checkPortmasterConnection allows all connection that originate from
// portmaster itself.
func checkPortmasterConnection(ctx context.Context, conn *network.Connection, _ *profile.LayeredProfile, _ packet.Packet) bool {
	// Grant own outgoing or local connections.

	// Blocking our own connections can lead to a very literal deadlock.
	// This can currently happen, as fast-tracked connections are also
	// reset in the OS integration and might show up in the connection
	// handling if a packet in the other direction hits the firewall first.

	// Ignore other processes.
	if conn.Process().Pid != ownPID {
		return false
	}

	// Ignore inbound connection if non-local.
	if conn.Inbound {
		myIP, err := netenv.IsMyIP(conn.Entity.IP)
		if err != nil {
			log.Tracer(ctx).Debugf("filter: failed to check if %s is own IP for granting own connection: %s", conn.Entity.IP, err)
			return false
		}
		if !myIP {
			return false
		}
	}

	log.Tracer(ctx).Infof("filter: granting own connection %s", conn)
	conn.Accept("connection by Portmaster", noReasonOptionKey)
	conn.Internal = true
	return true
}

func checkIfBroadcastReply(ctx context.Context, conn *network.Connection, _ *profile.LayeredProfile, _ packet.Packet) bool {
	// Only check inbound connections.
	if !conn.Inbound {
		return false
	}
	// Only check if the process has been identified.
	if !conn.Process().IsIdentified() {
		return false
	}

	// Check if the remote IP is part of a local network.
	localNet, err := netenv.GetLocalNetwork(conn.Entity.IP)
	if err != nil {
		log.Tracer(ctx).Warningf("filter: failed to get local network: %s", err)
		return false
	}
	if localNet == nil {
		return false
	}

	// Search for a matching requesting connection.
	requestingConn := network.GetMulticastRequestConn(conn, localNet)
	if requestingConn == nil {
		return false
	}

	conn.Accept(
		fmt.Sprintf(
			"response to multi/broadcast query to %s/%s",
			packet.IPProtocol(requestingConn.Entity.Protocol),
			net.JoinHostPort(
				requestingConn.Entity.IP.String(),
				strconv.Itoa(int(requestingConn.Entity.Port)),
			),
		),
		"",
	)
	return true
}

func checkEndpointLists(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// DNS request from the system resolver require a special decision process,
	// because the original requesting process is not known. Here, we only check
	// global-only and the most important per-app aspects. The resulting
	// connection is then blocked when the original requesting process is known.
	if conn.Type == network.DNSRequest && conn.Process().IsSystemResolver() {
		return checkEndpointListsForSystemResolverDNSRequests(ctx, conn, p)
	}

	var result endpoints.EPResult
	var reason endpoints.Reason

	// check endpoints list
	var optionKey string
	if conn.Inbound {
		result, reason = p.MatchServiceEndpoint(ctx, conn.Entity)
		optionKey = profile.CfgOptionServiceEndpointsKey
	} else {
		result, reason = p.MatchEndpoint(ctx, conn.Entity)
		optionKey = profile.CfgOptionEndpointsKey
	}
	switch result {
	case endpoints.Denied, endpoints.MatchError:
		conn.DenyWithContext(reason.String(), optionKey, reason.Context())
		return true
	case endpoints.Permitted:
		conn.AcceptWithContext(reason.String(), optionKey, reason.Context())
		return true
	case endpoints.NoMatch:
		return false
	}

	return false
}

// checkEndpointListsForSystemResolverDNSRequests is a special version of
// checkEndpointLists that is only meant for DNS queries by the system
// resolver. It only checks the endpoint filter list of the local profile and
// does not include the global profile.
func checkEndpointListsForSystemResolverDNSRequests(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile) bool {
	var profileEndpoints endpoints.Endpoints
	var optionKey string
	if conn.Inbound {
		profileEndpoints = p.LocalProfileWithoutLocking().GetServiceEndpoints()
		optionKey = profile.CfgOptionServiceEndpointsKey
	} else {
		profileEndpoints = p.LocalProfileWithoutLocking().GetEndpoints()
		optionKey = profile.CfgOptionEndpointsKey
	}

	if profileEndpoints.IsSet() {
		result, reason := profileEndpoints.Match(ctx, conn.Entity)
		if endpoints.IsDecision(result) {
			switch result {
			case endpoints.Denied, endpoints.MatchError:
				conn.DenyWithContext(reason.String(), optionKey, reason.Context())
				return true
			case endpoints.Permitted:
				conn.AcceptWithContext(reason.String(), optionKey, reason.Context())
				return true
			case endpoints.NoMatch:
				return false
			}
		}
	}

	return false
}

var p2pFilterLists = []string{"17-P2P"}

func checkConnectionType(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	switch {
	// Block incoming connection, if not from localhost.
	case p.BlockInbound() && conn.Inbound &&
		!conn.Entity.IPScope.IsLocalhost():
		conn.Drop("inbound connections blocked", profile.CfgOptionBlockInboundKey)
		return true

		// Check for P2P and related connections.
	case p.BlockP2P() && !conn.Inbound:
		switch {
		// Block anything that is in the P2P filter list.
		case conn.Entity.MatchLists(p2pFilterLists):
			conn.Block("P2P assistive infrastructure blocked based on filter list", profile.CfgOptionBlockP2PKey)
			return true

			// Remaining P2P deciders only apply to IP connections.
		case conn.Type != network.IPConnection:
			return false

			// Block well known ports of P2P assistive infrastructure.
		case conn.Entity.DstPort() == 3478 || // STUN/TURN
			conn.Entity.DstPort() == 5349: // STUN/TURN over TLS/DTLS
			conn.Block("P2P assistive infrastructure blocked based on port", profile.CfgOptionBlockP2PKey)
			return true

			// Block direct connections with not previous DNS request.
		case conn.Entity.IPScope.IsGlobal() &&
			conn.Entity.Domain == "":
			conn.Block("direct connections (P2P) blocked", profile.CfgOptionBlockP2PKey)
			return true

		default:
			return false
		}

	default:
		return false
	}
}

func checkConnectivityDomain(_ context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	switch {
	case conn.Entity.Domain == "":
		// Only applies if a domain is available.
		return false

	case netenv.GetOnlineStatus() > netenv.StatusPortal:
		// Special grant only applies if network status is Portal (or even more limited).
		return false

	case conn.Inbound:
		// Special grant only applies to outgoing connections.
		return false

	case p.BlockScopeInternet():
		// Special grant only applies if application is allowed to connect to the Internet.
		return false

	case netenv.IsConnectivityDomain(conn.Entity.Domain):
		// Special grant!
		conn.Accept("special grant for connectivity domain during network bootstrap", noReasonOptionKey)
		return true

	default:
		// Not a special grant domain
		return false
	}
}

func checkConnectionScope(_ context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// If we are handling a DNS request, check if we can immediately block it.
	if conn.Type == network.DNSRequest {
		// DNS is expected to resolve to LAN or Internet addresses.
		// Localhost queries are immediately responded to by the nameserver.
		if p.BlockScopeInternet() && p.BlockScopeLAN() {
			conn.Block("Internet and LAN access blocked", profile.CfgOptionBlockScopeInternetKey)
			return true
		}

		return false
	}

	// Check if the network scope is permitted.
	switch conn.Entity.IPScope {
	case netutils.Global, netutils.GlobalMulticast:
		if p.BlockScopeInternet() {
			conn.Deny("Internet access blocked", profile.CfgOptionBlockScopeInternetKey) // Block Outbound / Drop Inbound
			return true
		}
	case netutils.SiteLocal, netutils.LinkLocal, netutils.LocalMulticast:
		if p.BlockScopeLAN() {
			conn.Block("LAN access blocked", profile.CfgOptionBlockScopeLANKey) // Block Outbound / Drop Inbound
			return true
		}
	case netutils.HostLocal:
		if p.BlockScopeLocal() {
			conn.Block("Localhost access blocked", profile.CfgOptionBlockScopeLocalKey) // Block Outbound / Drop Inbound
			return true
		}
	case netutils.Undefined, netutils.Invalid:
		// Block Invalid / Undefined IPs _after_ the rules.
		return false
	default:
		conn.Deny("invalid IP", noReasonOptionKey) // Block Outbound / Drop Inbound
		return true
	}

	return false
}

func checkInvalidIP(_ context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// Only applies to IP connections.
	if conn.Type != network.IPConnection {
		return false
	}

	// Block Invalid / Undefined IPs.
	switch conn.Entity.IPScope { //nolint:exhaustive // Only looking for specific values.
	case netutils.Undefined, netutils.Invalid:
		conn.Deny("invalid IP", noReasonOptionKey) // Block Outbound / Drop Inbound
		return true
	}

	return false
}

func checkBypassPrevention(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	if p.PreventBypassing() {
		// check for bypass protection
		result, reason, reasonCtx := PreventBypassing(ctx, conn)
		switch result {
		case endpoints.Denied, endpoints.MatchError:
			// Also block on MatchError to be on the safe side.
			// PreventBypassing does not use any data that needs to be loaded, so it should not fail anyway.
			conn.BlockWithContext("bypass prevention: "+reason, profile.CfgOptionPreventBypassingKey, reasonCtx)
			return true
		case endpoints.Permitted:
			conn.AcceptWithContext("bypass prevention: "+reason, profile.CfgOptionPreventBypassingKey, reasonCtx)
			return true
		case endpoints.NoMatch:
			return false
		}
	}

	return false
}

func checkFilterLists(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// apply privacy filter lists
	result, reason := p.MatchFilterLists(ctx, conn.Entity)
	switch result {
	case endpoints.Denied:
		// If the connection matches a filter list, check if the "unbreak" list matches too and abort blocking.
		resolvedUnbreakFilterListIDs := filterlists.GetUnbreakFilterListIDs()
		for _, blockedListID := range conn.Entity.BlockedByLists {
			for _, unbreakListID := range resolvedUnbreakFilterListIDs {
				if blockedListID == unbreakListID {
					log.Tracer(ctx).Debugf("filter: unbreak filter %s matched, ignoring other filter list matches", unbreakListID)
					return false
				}
			}
		}
		// Otherwise, continue with blocking.
		conn.DenyWithContext(reason.String(), profile.CfgOptionFilterListsKey, reason.Context())
		return true
	case endpoints.NoMatch:
		// nothing to do
	case endpoints.Permitted, endpoints.MatchError:
		fallthrough
	default:
		log.Tracer(ctx).Debugf("filter: filter lists returned unsupported verdict: %s", result)
	}
	return false
}

func checkResolverScope(_ context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// If the IP address was resolved, check the scope of the resolver.
	switch {
	case conn.Type != network.IPConnection:
		// Only applies to IP connections.
	case !p.RemoveOutOfScopeDNS():
		// Out of scope checking is not active.
	case conn.Resolver == nil:
		// IP address of connection was not resolved.
	case conn.Resolver.IPScope.IsGlobal() &&
		(conn.Entity.IPScope.IsLAN() || conn.Entity.IPScope.IsLocalhost()):
		// Block global resolvers from returning LAN/Localhost IPs.
		conn.Block("DNS server horizon violation: global DNS server returned local IP address", profile.CfgOptionRemoveOutOfScopeDNSKey)
		return true
	case conn.Resolver.IPScope.IsLAN() &&
		conn.Entity.IPScope.IsLocalhost():
		// Block LAN resolvers from returning Localhost IPs.
		conn.Block("DNS server horizon violation: LAN DNS server returned localhost IP address", profile.CfgOptionRemoveOutOfScopeDNSKey)
		return true
	}

	return false
}

func checkDomainHeuristics(ctx context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// Don't check domain heuristics if no domain is available.
	if conn.Entity.Domain == "" {
		return false
	}

	// Check if domain heuristics are enabled.
	if !p.DomainHeuristics() {
		return false
	}

	trimmedDomain := strings.TrimRight(conn.Entity.Domain, ".")
	etld1, err := publicsuffix.EffectiveTLDPlusOne(trimmedDomain)
	if err != nil {
		// Don't run the check if the domain is a TLD.
		return false
	}

	domainToCheck := strings.Split(etld1, ".")[0]
	score := dga.LmsScore(domainToCheck)
	if score < 5 {
		log.Tracer(ctx).Debugf(
			"filter: possible data tunnel by %s in eTLD+1 %s: %s has an lms score of %.2f",
			conn.Process(),
			etld1,
			domainToCheck,
			score,
		)
		conn.Block("possible DGA domain commonly used by malware", profile.CfgOptionDomainHeuristicsKey)
		return true
	}
	log.Tracer(ctx).Tracef("filter: LMS score of eTLD+1 %s is %.2f", etld1, score)

	// 100 is a somewhat arbitrary threshold to ensure we don't mess
	// around with CDN domain names to early. They use short second-level
	// domains that would trigger LMS checks but are to small to actually
	// exfiltrate data.
	if len(conn.Entity.Domain) > len(etld1)+100 {
		domainToCheck = trimmedDomain[0:len(etld1)]
		score := dga.LmsScoreOfDomain(domainToCheck)
		if score < 10 {
			log.Tracer(ctx).Debugf(
				"filter: possible data tunnel by %s in subdomain of %s: %s has an lms score of %.2f",
				conn.Process(),
				conn.Entity.Domain,
				domainToCheck,
				score,
			)
			conn.Block("possible data tunnel for covert communication and protection bypassing", profile.CfgOptionDomainHeuristicsKey)
			return true
		}
		log.Tracer(ctx).Tracef("filter: LMS score of entire domain is %.2f", score)
	}

	return false
}

func checkAutoPermitRelated(_ context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// Auto permit is disabled for default action permit.
	if p.DefaultAction() == profile.DefaultActionPermit {
		return false
	}

	// Check if auto permit is disabled.
	if p.DisableAutoPermit() {
		return false
	}

	// Check for relation to auto permit.
	related, reason := checkRelation(conn)
	if related {
		conn.Accept(reason, profile.CfgOptionDisableAutoPermitKey)
		return true
	}

	return false
}

// checkRelation tries to find a relation between a process and a communication. This is for better out of the box experience and is _not_ meant to thwart intentional malware.
func checkRelation(conn *network.Connection) (related bool, reason string) {
	// Don't check relation if no domain is available.
	if conn.Entity.Domain == "" {
		return false, ""
	}
	// Don't check for unknown processes.
	if conn.Process().Pid < 0 {
		return false, ""
	}

	pathElements := strings.Split(conn.Process().Path, string(filepath.Separator))
	// only look at the last two path segments
	if len(pathElements) > 2 {
		pathElements = pathElements[len(pathElements)-2:]
	}
	domainElements := strings.Split(conn.Entity.Domain, ".")

	var domainElement string
	var processElement string

matchLoop:
	for _, domainElement = range domainElements {
		for _, pathElement := range pathElements {
			if levenshtein.Match(domainElement, pathElement, nil) > 0.5 {
				related = true
				processElement = pathElement
				break matchLoop
			}
		}
		if levenshtein.Match(domainElement, conn.Process().Name, nil) > 0.5 {
			related = true
			processElement = conn.Process().Name
			break matchLoop
		}
		if levenshtein.Match(domainElement, conn.Process().ExecName, nil) > 0.5 {
			related = true
			processElement = conn.Process().ExecName
			break matchLoop
		}
	}

	if related {
		reason = fmt.Sprintf("auto allowed: domain is related to process: %s is related to %s", domainElement, processElement)
	}
	return related, reason
}

func checkCustomFilterList(_ context.Context, conn *network.Connection, p *profile.LayeredProfile, _ packet.Packet) bool {
	// Check if any custom list is loaded at all.
	if !customlists.IsLoaded() {
		return false
	}

	// block if the domain name appears in the custom filter list (check for subdomains if enabled)
	if conn.Entity.Domain != "" {
		if ok, match := customlists.LookupDomain(conn.Entity.Domain, p.FilterSubDomains()); ok {
			conn.Deny(fmt.Sprintf("domain %s matches %s in custom filter list", conn.Entity.Domain, match), customlists.CfgOptionCustomListFileKey)
			return true
		}
	}

	// block if any of the CNAME appears in the custom filter list (check for subdomains if enabled)
	if p.FilterCNAMEs() {
		for _, cname := range conn.Entity.CNAME {
			if ok, match := customlists.LookupDomain(cname, p.FilterSubDomains()); ok {
				conn.Deny(fmt.Sprintf("domain alias (CNAME) %s matches %s in custom filter list", cname, match), customlists.CfgOptionCustomListFileKey)
				return true
			}
		}
	}

	// block if ip addresses appears in the custom filter list
	if conn.Entity.IP != nil {
		if customlists.LookupIP(conn.Entity.IP) {
			conn.Deny("IP address is in the custom filter list", customlists.CfgOptionCustomListFileKey)
			return true
		}
	}

	// block autonomous system by its number if it appears in the custom filter list
	if conn.Entity.ASN != 0 {
		if customlists.LookupASN(conn.Entity.ASN) {
			conn.Deny("AS is in the custom filter list", customlists.CfgOptionCustomListFileKey)
			return true
		}
	}

	// block if the country appears in the custom filter list
	if conn.Entity.Country != "" {
		if customlists.LookupCountry(conn.Entity.Country) {
			conn.Deny("country is in the custom filter list", customlists.CfgOptionCustomListFileKey)
			return true
		}
	}

	return false
}
