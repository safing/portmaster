package resolver

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/netutils"
)

const maxSearchDomains = 100

// Scope defines a domain scope and which resolvers can resolve it.
type Scope struct {
	Domain    string
	Resolvers []*Resolver
}

const (
	parameterName       = "name"
	parameterVerify     = "verify"
	parameterBlockedIf  = "blockedif"
	parameterSearch     = "search"
	parameterSearchOnly = "search-only"
	parameterPath       = "path"
)

var (
	globalResolvers []*Resolver          // all (global) resolvers
	localResolvers  []*Resolver          // all resolvers that are in site-local or link-local IP ranges
	systemResolvers []*Resolver          // all resolvers that were assigned by the system
	localScopes     []*Scope             // list of scopes with a list of local resolvers that can resolve the scope
	activeResolvers map[string]*Resolver // lookup map of all resolvers
	resolversLock   sync.RWMutex
)

func indexOfScope(domain string, list []*Scope) int {
	for k, v := range list {
		if v.Domain == domain {
			return k
		}
	}
	return -1
}

func getActiveResolverByIDWithLocking(server string) *Resolver {
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	resolver, ok := activeResolvers[server]
	if ok {
		return resolver
	}
	return nil
}

func formatIPAndPort(ip net.IP, port uint16) string {
	var address string
	if ipv4 := ip.To4(); ipv4 != nil {
		address = fmt.Sprintf("%s:%d", ipv4.String(), port)
	} else {
		address = fmt.Sprintf("[%s]:%d", ip.String(), port)
	}
	return address
}

func resolverConnFactory(resolver *Resolver) ResolverConn {
	switch resolver.Info.Type {
	case ServerTypeTCP:
		return NewTCPResolver(resolver)
	case ServerTypeDoT:
		return NewTCPResolver(resolver).UseTLS()
	case ServerTypeDoH:
		return NewHttpsResolver(resolver)
	case ServerTypeDNS:
		return NewPlainResolver(resolver)
	default:
		return nil
	}
}

func createResolver(resolverURL, source string) (*Resolver, bool, error) {
	u, err := url.Parse(resolverURL)
	if err != nil {
		return nil, false, err
	}

	switch u.Scheme {
	case ServerTypeDNS, ServerTypeDoT, ServerTypeDoH, ServerTypeTCP:
	default:
		return nil, false, fmt.Errorf("DNS resolver scheme %q invalid", u.Scheme)
	}

	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		return nil, false, fmt.Errorf("resolver IP %q invalid", u.Hostname())
	}

	// Add default port for scheme if it is missing.
	var port uint16
	hostPort := u.Port()
	switch {
	case hostPort != "":
		parsedPort, err := strconv.ParseUint(hostPort, 10, 16)
		if err != nil {
			return nil, false, fmt.Errorf("resolver port %q invalid", u.Port())
		}
		port = uint16(parsedPort)
	case u.Scheme == ServerTypeDNS, u.Scheme == ServerTypeTCP:
		port = 53
	case u.Scheme == ServerTypeDoH:
		port = 443
	case u.Scheme == ServerTypeDoT:
		port = 853
	default:
		return nil, false, fmt.Errorf("missing port in %q", u.Host)
	}

	scope := netutils.GetIPScope(ip)
	// Skip localhost resolvers from the OS, but not if configured.
	if scope.IsLocalhost() && source == ServerSourceOperatingSystem {
		return nil, true, nil // skip
	}

	// Get parameters and check if keys exist.
	query := u.Query()
	for key := range query {
		switch key {
		case parameterName,
			parameterVerify,
			parameterBlockedIf,
			parameterSearch,
			parameterSearchOnly,
			parameterPath:
			// Known key, continue.
		default:
			// Unknown key, abort.
			return nil, false, fmt.Errorf(`unknown parameter "%s"`, key)
		}
	}

	// Check domain verification config.
	verifyDomain := query.Get(parameterVerify)
	if verifyDomain != "" && !(u.Scheme == ServerTypeDoT || u.Scheme == ServerTypeDoH) {
		return nil, false, fmt.Errorf("domain verification only supported in DoT and DoH")
	}
	if verifyDomain == "" && (u.Scheme == ServerTypeDoT || u.Scheme == ServerTypeDoH) {
		return nil, false, fmt.Errorf("DOT must have a verify query parameter set")
	}

	// Check path for https (doh) request
	path := query.Get(parameterPath)
	if path != "" && u.Scheme != "doh" {
		return nil, false, fmt.Errorf("path parameter is only supported in DoH")
	}

	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Check block detection type.
	blockType := query.Get(parameterBlockedIf)
	if blockType == "" {
		blockType = BlockDetectionZeroIP
	}
	switch blockType {
	case BlockDetectionDisabled, BlockDetectionEmptyAnswer, BlockDetectionRefused, BlockDetectionZeroIP:
	default:
		return nil, false, fmt.Errorf("invalid value for upstream block detection (blockedif=)")
	}

	// Build resolver.
	newResolver := &Resolver{
		ConfigURL: resolverURL,
		Info: &ResolverInfo{
			Name:    query.Get(parameterName),
			Type:    u.Scheme,
			Source:  source,
			IP:      ip,
			IPScope: scope,
			Port:    port,
		},
		ServerAddress:          net.JoinHostPort(ip.String(), strconv.Itoa(int(port))),
		VerifyDomain:           verifyDomain,
		Path:                   path,
		UpstreamBlockDetection: blockType,
	}

	// Parse search domains.
	searchDomains := query.Get(parameterSearch)
	if searchDomains != "" {
		err = configureSearchDomains(newResolver, strings.Split(searchDomains, ","), true)
		if err != nil {
			return nil, false, err
		}
	}

	// Check if searchOnly is set and valid.
	if query.Has(parameterSearchOnly) {
		newResolver.SearchOnly = true
		if query.Get(parameterSearchOnly) != "" {
			return nil, false, fmt.Errorf("%s may only be used as an empty parameter", parameterSearchOnly)
		}
		if len(newResolver.Search) == 0 {
			return nil, false, fmt.Errorf("cannot use %s without search scopes", parameterSearchOnly)
		}
	}

	newResolver.Conn = resolverConnFactory(newResolver)
	return newResolver, false, nil
}

func configureSearchDomains(resolver *Resolver, searches []string, hardfail bool) error {
	resolver.Search = make([]string, 0, len(searches))

	// Check all search domains.
	for i, value := range searches {
		trimmedDomain := strings.ToLower(strings.Trim(value, "."))
		err := checkSearchScope(trimmedDomain)
		if err != nil {
			if hardfail {
				resolver.Search = nil
				return fmt.Errorf("failed to validate search domain #%d: %w", i+1, err)
			}
			log.Warningf("resolver: skipping invalid search domain for resolver %s: %s", resolver, utils.SafeFirst16Chars(value))
		} else {
			resolver.Search = append(resolver.Search, fmt.Sprintf(".%s.", trimmedDomain))
		}
	}

	// Limit search domains to mitigate exploitation via malicious local resolver.
	if len(resolver.Search) > maxSearchDomains {
		if hardfail {
			return fmt.Errorf("too many search domains, for security reasons only %d search domains are supported", maxSearchDomains)
		}
		log.Warningf("resolver: limiting search domains for resolver %s to %d for security reasons", resolver, maxSearchDomains)
		resolver.Search = resolver.Search[:maxSearchDomains]
	}

	return nil
}

func getConfiguredResolvers(list []string) (resolvers []*Resolver) {
	for _, server := range list {
		resolver, skip, err := createResolver(server, ServerSourceConfigured)
		if err != nil {
			// TODO(ppacher): module error
			log.Errorf("cannot use resolver %s: %s", server, err)
			continue
		}

		if skip {
			continue
		}

		resolvers = append(resolvers, resolver)
	}
	return resolvers
}

func getSystemResolvers() (resolvers []*Resolver) {
	for _, nameserver := range netenv.Nameservers() {
		serverURL := fmt.Sprintf("dns://%s", formatIPAndPort(nameserver.IP, 53))
		resolver, skip, err := createResolver(serverURL, ServerSourceOperatingSystem)
		if err != nil {
			// that shouldn't happen but handle it anyway ...
			log.Errorf("cannot use system resolver %s: %s", serverURL, err)
			continue
		}

		if skip {
			continue
		}

		if resolver.Info.IPScope.IsLAN() {
			_ = configureSearchDomains(resolver, nameserver.Search, false)
		}

		resolvers = append(resolvers, resolver)
	}
	return resolvers
}

const missingResolversErrorID = "missing-resolvers"

func loadResolvers() {
	// TODO: what happens when a lot of processes want to reload at once? we do not need to run this multiple times in a short time frame.
	resolversLock.Lock()
	defer resolversLock.Unlock()

	// Resolve module error about missing resolvers.
	module.Resolve(missingResolversErrorID)

	newResolvers := append(
		getConfiguredResolvers(configuredNameServers()),
		getSystemResolvers()...,
	)

	if len(newResolvers) == 0 {
		log.Warning("resolver: no (valid) dns server found in config or system, falling back to global defaults")
		module.Warning(
			missingResolversErrorID,
			"Using Factory Default DNS Servers",
			"The Portmaster could not find any (valid) DNS servers in the settings or system. In order to prevent being disconnected, the factory defaults are being used instead.",
		)

		// load defaults directly, overriding config system
		newResolvers = getConfiguredResolvers(defaultNameServers)
		if len(newResolvers) == 0 {
			log.Critical("resolver: no (valid) dns server found in config, system or global defaults")
			module.Error(
				missingResolversErrorID,
				"No DNS Server Configured",
				"The Portmaster could not find any (valid) DNS servers in the settings or system. You will experience severe connectivity problems until resolved.",
			)
		}
	}

	// save resolvers
	globalResolvers = newResolvers

	// assing resolvers to scopes
	setScopedResolvers(globalResolvers)

	// set active resolvers (for cache validation)
	// reset
	activeResolvers = make(map[string]*Resolver)
	// add
	for _, resolver := range newResolvers {
		activeResolvers[resolver.Info.ID()] = resolver
	}
	activeResolvers[mDNSResolver.Info.ID()] = mDNSResolver
	activeResolvers[envResolver.Info.ID()] = envResolver

	// log global resolvers
	if len(globalResolvers) > 0 {
		log.Trace("resolver: loaded global resolvers:")
		for _, resolver := range globalResolvers {
			log.Tracef("resolver: %s", resolver.ConfigURL)
		}
	} else {
		log.Warning("resolver: no global resolvers loaded")
	}

	// log local resolvers
	if len(localResolvers) > 0 {
		log.Trace("resolver: loaded local resolvers:")
		for _, resolver := range localResolvers {
			log.Tracef("resolver: %s", resolver.ConfigURL)
		}
	} else {
		log.Info("resolver: no local resolvers loaded")
	}

	// log system resolvers
	if len(systemResolvers) > 0 {
		log.Trace("resolver: loaded system/network-assigned resolvers:")
		for _, resolver := range systemResolvers {
			log.Tracef("resolver: %s", resolver.ConfigURL)
		}
	} else {
		log.Info("resolver: no system/network-assigned resolvers loaded")
	}

	// log scopes
	if len(localScopes) > 0 {
		log.Trace("resolver: loaded scopes:")
		for _, scope := range localScopes {
			var scopeServers []string
			for _, resolver := range scope.Resolvers {
				scopeServers = append(scopeServers, resolver.ConfigURL)
			}
			log.Tracef("resolver: %s: %s", scope.Domain, strings.Join(scopeServers, ", "))
		}
	} else {
		log.Info("resolver: no scopes loaded")
	}

	// alert if no resolvers are loaded
	if len(globalResolvers) == 0 && len(localResolvers) == 0 {
		log.Critical("resolver: no resolvers loaded!")
	}
}

func setScopedResolvers(resolvers []*Resolver) {
	// make list with local resolvers
	localResolvers = make([]*Resolver, 0)
	systemResolvers = make([]*Resolver, 0)
	localScopes = make([]*Scope, 0)

	for _, resolver := range resolvers {
		if resolver.Info.IPScope.IsLAN() {
			localResolvers = append(localResolvers, resolver)
		}

		if resolver.Info.Source == ServerSourceOperatingSystem {
			systemResolvers = append(systemResolvers, resolver)
		}

		if resolver.Search != nil {
			// add resolver to custom searches
			for _, search := range resolver.Search {
				if search == "." {
					continue
				}
				key := indexOfScope(search, localScopes)
				if key == -1 {
					localScopes = append(localScopes, &Scope{
						Domain:    search,
						Resolvers: []*Resolver{resolver},
					})
					continue
				}
				localScopes[key].Resolvers = append(localScopes[key].Resolvers, resolver)
			}
		}
	}

	// sort scopes by length
	sort.Slice(localScopes,
		func(i, j int) bool {
			return len(localScopes[i].Domain) > len(localScopes[j].Domain)
		},
	)
}

func checkSearchScope(searchDomain string) error {
	// Sanity check the input.
	if len(searchDomain) == 0 ||
		searchDomain[0] == '.' ||
		searchDomain[len(searchDomain)-1] == '.' {
		return fmt.Errorf("invalid (1) search domain: %s", searchDomain)
	}

	// Domains that are specifically listed in the special service domains may be
	// diverted completely.
	if domainInScope("."+searchDomain+".", specialServiceDomains) {
		return nil
	}

	// In order to check if the search domain is too high up in the hierarchy, we
	// need to add some more subdomains.
	augmentedSearchDomain := "*.*.*.*.*." + searchDomain

	// Get the public suffix of the search domain and if the TLD is managed by ICANN.
	suffix, icann := publicsuffix.PublicSuffix(augmentedSearchDomain)
	// Sanity check the result.
	if len(suffix) == 0 {
		return fmt.Errorf("invalid (2) search domain: %s", searchDomain)
	}

	// TLDs that are not managed by ICANN (ie. are unofficial) may be
	// diverted completely.
	// This might include special service domains like ".onion" and ".bit".
	if !icann && !strings.Contains(suffix, ".") {
		return nil
	}

	// Build the eTLD+1 domain, which is the highest that may be diverted.
	split := len(augmentedSearchDomain) - len(suffix) - 1
	eTLDplus1 := augmentedSearchDomain[1+strings.LastIndex(augmentedSearchDomain[:split], "."):]

	// Check if the scope is being violated by checking if the eTLD+1 contains a wildcard.
	if strings.Contains(eTLDplus1, "*") {
		return fmt.Errorf(`search domain "%s" is dangerously high up the hierarchy, stay at or below "%s"`, searchDomain, eTLDplus1)
	}

	return nil
}

// IsResolverAddress returns whether the given ip and port match a configured resolver.
func IsResolverAddress(ip net.IP, port uint16) bool {
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	// Check if the given IP and port matches a resolver.
	for _, r := range globalResolvers {
		if port == r.Info.Port && r.Info.IP.Equal(ip) {
			return true
		}
	}

	return false
}
