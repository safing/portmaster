package resolver

import (
	"context"
	"errors"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
)

// Domain Scopes.
var (
	// Localhost Domain
	// Handling: Respond with 127.0.0.1 and ::1 to A and AAAA queries, respectively.
	// See RFC6761.
	localhostDomain = ".localhost."

	// Invalid Domain
	// Handling: Always respond with NXDOMAIN.
	// See RFC6761.
	invalidDomain = ".invalid."

	// Internal Special-Use Domain
	// Used by Portmaster for special addressing.
	internalSpecialUseDomains = []string{
		"." + InternalSpecialUseDomain,
	}

	// Multicast DNS
	// Handling: Send to nameservers with matching search scope, then MDNS
	// See RFC6762.
	multicastDomains = []string{
		".local.",
		".254.169.in-addr.arpa.",
		".8.e.f.ip6.arpa.",
		".9.e.f.ip6.arpa.",
		".a.e.f.ip6.arpa.",
		".b.e.f.ip6.arpa.",
	}

	// Special-Use Domain Names
	// Handling: Send to nameservers with matching search scope, then local and system assigned nameservers
	// IANA Ref: https://www.iana.org/assignments/special-use-domain-names
	specialUseDomains = []string{
		// RFC8375: Designated for non-unique use in residential home networks.
		".home.arpa.",

		// RFC6762 (Appendix G): Non-official, but officially listed, private use domains.
		".intranet.",
		".internal.",
		".private.",
		".corp.",
		".home.",
		".lan.",

		// RFC6761: IPv4 private-address reverse-mapping domains.
		".10.in-addr.arpa.",
		".16.172.in-addr.arpa.",
		".17.172.in-addr.arpa.",
		".18.172.in-addr.arpa.",
		".19.172.in-addr.arpa.",
		".20.172.in-addr.arpa.",
		".21.172.in-addr.arpa.",
		".22.172.in-addr.arpa.",
		".23.172.in-addr.arpa.",
		".24.172.in-addr.arpa.",
		".25.172.in-addr.arpa.",
		".26.172.in-addr.arpa.",
		".27.172.in-addr.arpa.",
		".28.172.in-addr.arpa.",
		".29.172.in-addr.arpa.",
		".30.172.in-addr.arpa.",
		".31.172.in-addr.arpa.",
		".168.192.in-addr.arpa.",

		// RFC4193: IPv6 private-address reverse-mapping domains.
		".d.f.ip6.arpa.",
		".c.f.ip6.arpa.",

		// RFC6761: Special use domains for documentation and testing.
		".example.",
		".example.com.",
		".example.net.",
		".example.org.",
		".test.",
	}

	// Special-Service Domain Names
	// Handling: Send to nameservers with matching search scope, then local and system assigned nameservers.
	specialServiceDomains = []string{
		// RFC7686: Tor Hidden Services, https://www.torproject.org/
		".onion.",

		// I2P: Fully encrypted private network layer, https://geti2p.net/
		".i2p.",

		// Lokinet: Internal services on the decentralised network, https://lokinet.org/
		".loki.",

		// Namecoin: Blockchain based nameservice, https://www.namecoin.org/
		".bit.",

		// Ethereum Name Service (ENS): naming system based on the Ethereum blockchain, https://ens.domains/
		".eth.",

		// Unstoppable Domains: NFT based domain names, https://unstoppabledomains.com/
		".888.",
		".bitcoin.",
		".coin.",
		".crypto.",
		".dao.",
		".nft.",
		".wallet.",
		".x.",
		".zil.",

		// EmerDNS: Domain name registration on EmerCoin, https://emercoin.com/en/emerdns/
		".bazar.",
		".coin.",
		".emc.",
		".lib.",

		// OpenNIC TLDs: Democratic alternative to ICANN, https://www.opennic.org/
		".bbs.",
		".chan.",
		".dyn.",
		".free.",
		".fur.",
		".geek.",
		".glue.",
		".gopher.",
		".indy.",
		".libre.",
		".neo.",
		".null.",
		".o.",
		".oss.",
		".oz.",
		".parody.",
		".pirate.",

		// NewNations: TLDs for countries/regions without a ccTLD, http://new-nations.net/
		".ku.",
		".te.",
		".ti.",
		".uu.",
	}
)

func domainInScope(dotPrefixedFQDN string, scopeList []string) bool {
	for _, scope := range scopeList {
		if strings.HasSuffix(dotPrefixedFQDN, scope) {
			return true
		}
	}
	return false
}

// GetResolversInScope returns all resolvers that are in scope the resolve the given query and options.
func GetResolversInScope(ctx context.Context, q *Query) (selected []*Resolver, primarySource string, tryAll bool) { //nolint:gocognit // TODO
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	// Internal use domains
	if domainInScope(q.dotPrefixedFQDN, internalSpecialUseDomains) {
		return envResolvers, ServerSourceEnv, false
	}

	// Special connectivity domains
	if netenv.IsConnectivityDomain(q.FQDN) && len(systemResolvers) > 0 {
		selected = addResolvers(ctx, q, selected, systemResolvers)
		if len(selected) == 0 {
			selected = addResolvers(ctx, q, selected, localResolvers)
			selected = addResolvers(ctx, q, selected, globalResolvers)
		}
		return selected, ServerSourceOperatingSystem, false
	}

	// Prioritize search scopes
	for _, scope := range localScopes {
		if strings.HasSuffix(q.dotPrefixedFQDN, scope.Domain) {
			selected = addResolvers(ctx, q, selected, scope.Resolvers)
		}
	}

	// Handle multicast domains
	if domainInScope(q.dotPrefixedFQDN, multicastDomains) {
		selected = addResolvers(ctx, q, selected, mDNSResolvers)
		selected = addResolvers(ctx, q, selected, localResolvers)
		selected = addResolvers(ctx, q, selected, systemResolvers)
		return selected, ServerSourceMDNS, true
	}

	// Special use domains
	if domainInScope(q.dotPrefixedFQDN, specialUseDomains) ||
		domainInScope(q.dotPrefixedFQDN, specialServiceDomains) {
		selected = addResolvers(ctx, q, selected, localResolvers)
		return selected, "special", true
	}

	// Global domains
	selected = addResolvers(ctx, q, selected, globalResolvers)
	return selected, ServerSourceConfigured, false
}

func addResolvers(ctx context.Context, q *Query, selected []*Resolver, addResolvers []*Resolver) []*Resolver {
addNextResolver:
	for _, resolver := range addResolvers {
		// check for compliance
		if err := resolver.checkCompliance(ctx, q); err != nil {
			log.Tracer(ctx).Tracef("skipping non-compliant resolver %s: %s", resolver.Info.DescriptiveName(), err)
			continue
		}

		// deduplicate
		for _, selectedResolver := range selected {
			if selectedResolver.Info.ID() == resolver.Info.ID() {
				continue addNextResolver
			}
		}

		// the domains from the configured resolvers should not be resolved with the same resolvers
		if resolver.Info.Source == ServerSourceConfigured && resolver.Info.IP == nil {
			if _, ok := resolverInitDomains[q.FQDN]; ok {
				continue addNextResolver
			}
		}

		// add compliant and unique resolvers to selected resolvers
		selected = append(selected, resolver)
	}
	return selected
}

var (
	errInsecureProtocol = errors.New("insecure protocols disabled")
	errAssignedServer   = errors.New("assigned (dhcp) nameservers disabled")
	errMulticastDNS     = errors.New("multicast DNS disabled")
	errOutOfScope       = errors.New("query out of scope for resolver")
)

func (q *Query) checkCompliance() error {
	// RFC6761 - always respond with nxdomain
	if strings.HasSuffix(q.dotPrefixedFQDN, invalidDomain) {
		return ErrNotFound
	}

	// RFC6761 - respond with 127.0.0.1 and ::1 to A and AAAA queries respectively, else nxdomain
	if strings.HasSuffix(q.dotPrefixedFQDN, localhostDomain) {
		switch uint16(q.QType) {
		case dns.TypeA, dns.TypeAAAA:
			return ErrLocalhost
		default:
			return ErrNotFound
		}
	}

	// special TLDs
	if dontResolveSpecialDomains() &&
		domainInScope(q.dotPrefixedFQDN, specialServiceDomains) {
		return ErrSpecialDomainsDisabled
	}

	return nil
}

func (resolver *Resolver) checkCompliance(_ context.Context, q *Query) error {
	if noInsecureProtocols() {
		switch resolver.Info.Type {
		case ServerTypeDNS:
			return errInsecureProtocol
		case ServerTypeTCP:
			return errInsecureProtocol
		case ServerTypeDoT:
			// compliant
		case ServerTypeDoH:
			// compliant
		case ServerTypeEnv:
			// compliant (data is sourced from local network only and is highly limited)
		default:
			return errInsecureProtocol
		}
	}

	if noAssignedNameservers() {
		if resolver.Info.Source == ServerSourceOperatingSystem {
			return errAssignedServer
		}
	}

	if noMulticastDNS() {
		if resolver.Info.Source == ServerSourceMDNS {
			return errMulticastDNS
		}
	}

	// Check if the resolver should only be used for the search scopes.
	if resolver.SearchOnly && !domainInScope(q.dotPrefixedFQDN, resolver.Search) {
		return errOutOfScope
	}

	return nil
}
