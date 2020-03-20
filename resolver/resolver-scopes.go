package resolver

import (
	"context"
	"errors"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
)

// special scopes:

// localhost. [RFC6761] - respond with 127.0.0.1 and ::1 to A and AAAA queries, else nxdomain

// local. [RFC6762] - resolve if search, else resolve with mdns
// 10.in-addr.arpa. [RFC6761]
// 16.172.in-addr.arpa. [RFC6761]
// 17.172.in-addr.arpa. [RFC6761]
// 18.172.in-addr.arpa. [RFC6761]
// 19.172.in-addr.arpa. [RFC6761]
// 20.172.in-addr.arpa. [RFC6761]
// 21.172.in-addr.arpa. [RFC6761]
// 22.172.in-addr.arpa. [RFC6761]
// 23.172.in-addr.arpa. [RFC6761]
// 24.172.in-addr.arpa. [RFC6761]
// 25.172.in-addr.arpa. [RFC6761]
// 26.172.in-addr.arpa. [RFC6761]
// 27.172.in-addr.arpa. [RFC6761]
// 28.172.in-addr.arpa. [RFC6761]
// 29.172.in-addr.arpa. [RFC6761]
// 30.172.in-addr.arpa. [RFC6761]
// 31.172.in-addr.arpa. [RFC6761]
// 168.192.in-addr.arpa. [RFC6761]
// 254.169.in-addr.arpa. [RFC6762]
// 8.e.f.ip6.arpa. [RFC6762]
// 9.e.f.ip6.arpa. [RFC6762]
// a.e.f.ip6.arpa. [RFC6762]
// b.e.f.ip6.arpa. [RFC6762]

// example. [RFC6761] - resolve if search, else return nxdomain
// example.com. [RFC6761] - resolve if search, else return nxdomain
// example.net. [RFC6761] - resolve if search, else return nxdomain
// example.org. [RFC6761] - resolve if search, else return nxdomain
// invalid. [RFC6761] - resolve if search, else return nxdomain
// test. [RFC6761] - resolve if search, else return nxdomain
// onion. [RFC7686] - resolve if search, else return nxdomain

// resolvers:
// local
// global
// mdns

var (
	// RFC6761 - respond with 127.0.0.1 and ::1 to A and AAAA queries respectively, else nxdomain
	localhost = ".localhost."

	// RFC6761 - always respond with nxdomain
	invalid = ".invalid."

	// RFC6762 - resolve locally
	local = ".local."

	// local reverse dns
	localReverseScopes = []string{
		".10.in-addr.arpa.",      // RFC6761
		".16.172.in-addr.arpa.",  // RFC6761
		".17.172.in-addr.arpa.",  // RFC6761
		".18.172.in-addr.arpa.",  // RFC6761
		".19.172.in-addr.arpa.",  // RFC6761
		".20.172.in-addr.arpa.",  // RFC6761
		".21.172.in-addr.arpa.",  // RFC6761
		".22.172.in-addr.arpa.",  // RFC6761
		".23.172.in-addr.arpa.",  // RFC6761
		".24.172.in-addr.arpa.",  // RFC6761
		".25.172.in-addr.arpa.",  // RFC6761
		".26.172.in-addr.arpa.",  // RFC6761
		".27.172.in-addr.arpa.",  // RFC6761
		".28.172.in-addr.arpa.",  // RFC6761
		".29.172.in-addr.arpa.",  // RFC6761
		".30.172.in-addr.arpa.",  // RFC6761
		".31.172.in-addr.arpa.",  // RFC6761
		".168.192.in-addr.arpa.", // RFC6761
		".254.169.in-addr.arpa.", // RFC6762
		".8.e.f.ip6.arpa.",       // RFC6762
		".9.e.f.ip6.arpa.",       // RFC6762
		".a.e.f.ip6.arpa.",       // RFC6762
		".b.e.f.ip6.arpa.",       // RFC6762
	}

	// RFC6761 - only resolve locally
	localTestScopes = []string{
		".example.",
		".example.com.",
		".example.net.",
		".example.org.",
		".test.",
	}

	// resolve globally - resolving these should be disabled by default
	specialServiceScopes = []string{
		".onion.", // Tor Hidden Services, RFC7686
		".bit.",   // Namecoin, https://www.namecoin.org/
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
func GetResolversInScope(ctx context.Context, q *Query) (selected []*Resolver) {
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	// resolver selection:
	// local -> local scopes, mdns
	// local-inaddr -> local, mdns
	// global -> local scopes, global
	// special -> local scopes, local

	// check local scopes
	for _, scope := range localScopes {
		if strings.HasSuffix(q.dotPrefixedFQDN, scope.Domain) {
			// scoped resolvers
			for _, resolver := range scope.Resolvers {
				if err := resolver.checkCompliance(ctx, q); err == nil {
					selected = append(selected, resolver)
				} else {
					log.Tracef("skipping non-compliant resolver: %s", resolver.Server)
				}
			}
		}
	}
	// if there was a match with a local scope, stop here
	if len(selected) > 0 {
		// add mdns
		if err := mDNSResolver.checkCompliance(ctx, q); err == nil {
			selected = append(selected, mDNSResolver)
		} else {
			log.Tracef("skipping non-compliant resolver: %s", mDNSResolver.Server)
		}
		return selected
	}

	// check local reverse scope
	if domainInScope(q.dotPrefixedFQDN, localReverseScopes) {
		// local resolvers
		for _, resolver := range localResolvers {
			if err := resolver.checkCompliance(ctx, q); err == nil {
				selected = append(selected, resolver)
			} else {
				log.Tracef("skipping non-compliant resolver: %s", resolver.Server)
			}
		}
		// mdns resolver
		if err := mDNSResolver.checkCompliance(ctx, q); err == nil {
			selected = append(selected, mDNSResolver)
		} else {
			log.Tracef("skipping non-compliant resolver: %s", mDNSResolver.Server)
		}
		return selected
	}

	// check for .local mdns
	if strings.HasSuffix(q.dotPrefixedFQDN, local) {
		// add mdns
		if err := mDNSResolver.checkCompliance(ctx, q); err == nil {
			selected = append(selected, mDNSResolver)
		} else {
			log.Tracef("skipping non-compliant resolver: %s", mDNSResolver.Server)
		}
		return selected
	}

	// check for test scopes
	if domainInScope(q.dotPrefixedFQDN, localTestScopes) {
		// local resolvers
		for _, resolver := range localResolvers {
			if err := resolver.checkCompliance(ctx, q); err == nil {
				selected = append(selected, resolver)
			} else {
				log.Tracef("skipping non-compliant resolver: %s", resolver.Server)
			}
		}
		return selected
	}

	// finally, query globally
	for _, resolver := range globalResolvers {
		if err := resolver.checkCompliance(ctx, q); err == nil {
			selected = append(selected, resolver)
		} else {
			log.Tracef("skipping non-compliant resolver: %s", resolver.Server)
		}
	}
	return selected
}

var (
	errInsecureProtocol = errors.New("insecure protocols disabled")
	errAssignedServer   = errors.New("assigned (dhcp) nameservers disabled")
	errMulticastDNS     = errors.New("multicast DNS disabled")
	errSkip             = errors.New("this fqdn cannot resolved by this resolver")
)

func (q *Query) checkCompliance() error {
	// RFC6761 - always respond with nxdomain
	if strings.HasSuffix(q.dotPrefixedFQDN, invalid) {
		return ErrNotFound
	}

	// RFC6761 - respond with 127.0.0.1 and ::1 to A and AAAA queries respectively, else nxdomain
	if strings.HasSuffix(q.dotPrefixedFQDN, localhost) {
		switch uint16(q.QType) {
		case dns.TypeA, dns.TypeAAAA:
			return ErrLocalhost
		default:
			return ErrNotFound
		}
	}

	// special TLDs
	if doNotResolveSpecialDomains(q.SecurityLevel) &&
		domainInScope(q.dotPrefixedFQDN, specialServiceScopes) {
		return ErrSpecialDomainsDisabled
	}

	// testing TLDs
	if doNotResolveTestDomains(q.SecurityLevel) &&
		domainInScope(q.dotPrefixedFQDN, localTestScopes) {
		return ErrTestDomainsDisabled
	}

	return nil
}

func (resolver *Resolver) checkCompliance(_ context.Context, q *Query) error {
	if q.FQDN == resolver.SkipFQDN {
		return errSkip
	}

	if doNotUseInsecureProtocols(q.SecurityLevel) {
		switch resolver.ServerType {
		case ServerTypeDNS:
			return errInsecureProtocol
		case ServerTypeTCP:
			return errInsecureProtocol
		case ServerTypeDoT:
			// compliant
		case ServerTypeDoH:
			// compliant
		default:
			return errInsecureProtocol
		}
	}

	if doNotUseAssignedNameservers(q.SecurityLevel) {
		if resolver.Source == ServerSourceAssigned {
			return errAssignedServer
		}
	}

	if doNotUseMulticastDNS(q.SecurityLevel) {
		if resolver.Source == ServerSourceMDNS {
			return errMulticastDNS
		}
	}

	return nil
}
