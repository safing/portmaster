package resolver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/miekg/dns"
	"golang.org/x/net/publicsuffix"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
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
	parameterIP         = "ip"
	parameterBlockedIf  = "blockedif"
	parameterSearch     = "search"
	parameterSearchOnly = "search-only"
	parameterLinkLocal  = "link-local"
)

var (
	globalResolvers       []*Resolver          // all (global) resolvers
	localResolvers        []*Resolver          // all resolvers that are in site-local or link-local IP ranges
	systemResolvers       []*Resolver          // all resolvers that were assigned by the system
	localScopes           []*Scope             // list of scopes with a list of local resolvers that can resolve the scope
	activeResolvers       map[string]*Resolver // lookup map of all resolvers
	currentResolverConfig []string             // current active resolver config, to detect changes
	resolverInitDomains   map[string]struct{}  // a set with all domains of the dns resolvers

	resolversLock sync.RWMutex
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
		return NewHTTPSResolver(resolver)
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

	if resolverInitDomains == nil {
		resolverInitDomains = make(map[string]struct{})
	}

	switch u.Scheme {
	case ServerTypeDNS, ServerTypeDoT, ServerTypeDoH, ServerTypeTCP:
	case HTTPSProtocol:
		u.Scheme = ServerTypeDoH
	case TLSProtocol:
		u.Scheme = ServerTypeDoT
	default:
		return nil, false, fmt.Errorf("DNS resolver scheme %q invalid", u.Scheme)
	}

	query := u.Query()

	// Create Resolver object
	newResolver := &Resolver{
		ConfigURL: resolverURL,
		Info: &ResolverInfo{
			Name:    query.Get(parameterName),
			Type:    u.Scheme,
			Source:  source,
			IP:      nil,
			Domain:  "",
			IPScope: netutils.Global,
			Port:    0,
		},
		ServerAddress:          "",
		Path:                   u.Path, // Used for DoH
		UpstreamBlockDetection: "",
	}

	// Get parameters and check if keys exist.
	err = checkAndSetResolverParamters(u, newResolver)
	if err != nil {
		return nil, false, err
	}

	// Check block detection type.
	newResolver.UpstreamBlockDetection = query.Get(parameterBlockedIf)
	if newResolver.UpstreamBlockDetection == "" {
		newResolver.UpstreamBlockDetection = BlockDetectionZeroIP
	}

	switch newResolver.UpstreamBlockDetection {
	case BlockDetectionDisabled, BlockDetectionEmptyAnswer, BlockDetectionRefused, BlockDetectionZeroIP:
	default:
		return nil, false, fmt.Errorf("invalid value for upstream block detection (blockedif=)")
	}

	// Get ip scope if we have ip
	if newResolver.Info.IP != nil {
		newResolver.Info.IPScope = netutils.GetIPScope(newResolver.Info.IP)
		// Skip localhost resolvers from the OS, but not if configured.
		if newResolver.Info.IPScope.IsLocalhost() && source == ServerSourceOperatingSystem {
			return nil, true, nil // skip
		}
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

	// Check if this is a link-local resolver.
	if query.Has(parameterLinkLocal) {
		if query.Get(parameterLinkLocal) != "" {
			return nil, false, fmt.Errorf("%s may only be used as an empty parameter", parameterLinkLocal)
		}
		// Check if resolver IP is link-local.
		resolverNet, err := netenv.GetLocalNetwork(newResolver.Info.IP)
		switch {
		case err != nil:
			newResolver.LinkLocalUnavailable = true
		case resolverNet == nil:
			newResolver.LinkLocalUnavailable = true
		}
	}

	newResolver.Conn = resolverConnFactory(newResolver)
	return newResolver, false, nil
}

func checkAndSetResolverParamters(u *url.URL, resolver *Resolver) error {
	// Check if we are using domain name and if it's in a valid scheme
	ip := net.ParseIP(u.Hostname())
	hostnameIsDomaion := (ip == nil)
	if ip == nil && u.Scheme != ServerTypeDoH && u.Scheme != ServerTypeDoT {
		return fmt.Errorf("resolver IP %q is invalid", u.Hostname())
	}

	// Add default port for scheme if it is missing.
	port, err := parsePortFromURL(u)
	if err != nil {
		return err
	}
	resolver.Info.Port = port
	resolver.Info.IP = ip

	query := u.Query()

	for key := range query {
		switch key {
		case parameterName,
			parameterVerify,
			parameterIP,
			parameterBlockedIf,
			parameterSearch,
			parameterSearchOnly,
			parameterLinkLocal:
			// Known key, continue.
		default:
			// Unknown key, abort.
			return fmt.Errorf(`unknown parameter "%q"`, key)
		}
	}

	resolver.Info.Domain = query.Get(parameterVerify)
	paramterServerIP := query.Get(parameterIP)

	if u.Scheme == ServerTypeDoT || u.Scheme == ServerTypeDoH {
		// Check if IP and Domain are set correctly
		switch {
		case hostnameIsDomaion && resolver.Info.Domain != "":
			return fmt.Errorf("cannot set the domain name via both the hostname in the URL and the verify parameter")
		case !hostnameIsDomaion && resolver.Info.Domain == "":
			return fmt.Errorf("verify parameter must be set when using ip as domain")
		case !hostnameIsDomaion && paramterServerIP != "":
			return fmt.Errorf("cannot set the IP address via both the hostname in the URL and the ip parameter")
		}

		// Parse and set IP and Domain to the resolver
		switch {
		case hostnameIsDomaion && paramterServerIP != "": // domain and ip as parameter
			resolver.Info.IP = net.ParseIP(paramterServerIP)
			resolver.ServerAddress = net.JoinHostPort(paramterServerIP, strconv.Itoa(int(resolver.Info.Port)))
			resolver.Info.Domain = u.Hostname()
		case !hostnameIsDomaion && resolver.Info.Domain != "": // ip and domain as parameter
			resolver.ServerAddress = net.JoinHostPort(ip.String(), strconv.Itoa(int(resolver.Info.Port)))
		case hostnameIsDomaion && resolver.Info.Domain == "" && paramterServerIP == "": // only domain
			resolver.Info.Domain = u.Hostname()
			resolver.ServerAddress = net.JoinHostPort(resolver.Info.Domain, strconv.Itoa(int(port)))
		}

		if ip == nil {
			resolverInitDomains[dns.Fqdn(resolver.Info.Domain)] = struct{}{}
		}

	} else {
		if resolver.Info.Domain != "" {
			return fmt.Errorf("domain verification is only supported by DoT and DoH servers")
		}
		resolver.ServerAddress = net.JoinHostPort(ip.String(), strconv.Itoa(int(resolver.Info.Port)))
	}

	return nil
}

func parsePortFromURL(url *url.URL) (uint16, error) {
	var port uint16
	hostPort := url.Port()
	if hostPort != "" {
		// There is a port in the url
		parsedPort, err := strconv.ParseUint(hostPort, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("invalid port %q", url.Port())
		}
		port = uint16(parsedPort)
	} else {
		// set the default port for the protocol
		switch {
		case url.Scheme == ServerTypeDNS, url.Scheme == ServerTypeTCP:
			port = 53
		case url.Scheme == ServerTypeDoH:
			port = 443
		case url.Scheme == ServerTypeDoT:
			port = 853
		default:
			return 0, fmt.Errorf("cannot determine port for %q", url.Scheme)
		}
	}

	return port, nil
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
	defer func() {
		if !allConfiguredResolversAreFailing() {
			resetFailingResolversNotification()
		}
	}()

	// TODO: what happens when a lot of processes want to reload at once? we do not need to run this multiple times in a short time frame.
	resolversLock.Lock()
	defer resolversLock.Unlock()

	// Resolve module error about missing resolvers.
	module.states.Remove(missingResolversErrorID)
	// Check if settings were changed and clear name cache when they did.
	newResolverConfig := configuredNameServers()
	if len(currentResolverConfig) > 0 &&
		!utils.StringSliceEqual(currentResolverConfig, newResolverConfig) {
		module.mgr.Go("clear dns cache", func(ctx *mgr.WorkerCtx) error {
			log.Info("resolver: clearing dns cache due to changed resolver config")
			_, err := clearNameCache(ctx.Ctx())
			return err
		})
	}

	// If no resolvers are configure set the disabled state. So other modules knows that the users does not want to use Portmaster resolver.
	if len(newResolverConfig) == 0 {
		module.isDisabled.Store(true)
	} else {
		module.isDisabled.Store(false)
	}

	currentResolverConfig = newResolverConfig

	newResolvers := append(
		getConfiguredResolvers(newResolverConfig),
		getSystemResolvers()...,
	)

	if len(newResolvers) == 0 {
		// load defaults directly, overriding config system
		newResolvers = getConfiguredResolvers(defaultNameServers)
		if len(newResolvers) > 0 {
			log.Warning("resolver: no (valid) dns server found in config or system, falling back to global defaults")
			module.states.Add(mgr.State{
				ID:      missingResolversErrorID,
				Name:    "Using Factory Default DNS Servers",
				Message: "The Portmaster could not find any (valid) DNS servers in the settings or system. In order to prevent being disconnected, the factory defaults are being used instead. If you just switched your network, this should be resolved shortly.",
				Type:    mgr.StateTypeWarning,
			})
		} else {
			log.Critical("resolver: no (valid) dns server found in config, system or global defaults")
			module.states.Add(mgr.State{
				ID:      missingResolversErrorID,
				Name:    "No DNS Servers Configured",
				Message: "The Portmaster could not find any (valid) DNS servers in the settings or system. You will experience severe connectivity problems until resolved. If you just switched your network, this should be resolved shortly.",
				Type:    mgr.StateTypeError,
			})
		}
	}

	// save resolvers
	globalResolvers = newResolvers

	// assign resolvers to scopes
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
		} else if net, _ := netenv.GetLocalNetwork(resolver.Info.IP); net != nil {
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

// ForceResolverReconnect forces all resolvers to reconnect.
func ForceResolverReconnect(ctx context.Context) {
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	ctx, tracer := log.AddTracer(ctx)
	defer tracer.Submit()

	tracer.Trace("resolver: forcing all active resolvers to reconnect")
	for _, r := range globalResolvers {
		r.Conn.ForceReconnect(ctx)
	}
	tracer.Info("resolver: all active resolvers were forced to reconnect")
}

// allConfiguredResolversAreFailing reports whether all configured resolvers are failing.
// Return false if there are no configured resolvers.
func allConfiguredResolversAreFailing() bool {
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	// If there are no configured resolvers, return as not failing.
	if len(currentResolverConfig) == 0 {
		return false
	}

	// Return as not failing, if we can find any non-failing configured resolver.
	for _, resolver := range globalResolvers {
		if !resolver.Conn.IsFailing() && resolver.Info.Source == ServerSourceConfigured {
			// We found a non-failing configured resolver.
			return false
		}
	}

	return true
}
