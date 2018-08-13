// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/Safing/safing-core/configuration"
	"github.com/Safing/safing-core/database"
	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/network/environment"
	"github.com/Safing/safing-core/network/netutils"
)

// TODO: make resolver interface for http package

// special tlds:

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

// scopes:
// local-inaddr -> local, mdns
// local -> local scopes, mdns
// global -> local scopes, global
// special -> local scopes, local

type Resolver struct {
	// static
	Server               string
	ServerAddress        string
	IP                   *net.IP
	Port                 uint16
	Resolve              func(resolver *Resolver, fqdn string, qtype dns.Type) (*RRCache, error)
	Search               *[]string
	AllowedSecurityLevel int8
	SkipFqdnBeforeInit   string
	HTTPClient           *http.Client
	Source               string

	// atomic
	Initialized *abool.AtomicBool
	InitLock    sync.Mutex
	LastFail    *int64
	Expires     *int64

	// must be locked
	LockReason sync.Mutex
	FailReason string

	// TODO: add:
	// Expiration (for server got from DHCP / ICMPv6)
	// bootstrapping (first query is already sent, wait for it to either succeed or fail - think about http bootstrapping here!)
	// expanded server info: type, server address, server port, options - so we do not have to parse this every time!
}

func (r *Resolver) String() string {
	return r.Server
}

func (r *Resolver) Address() string {
	return urlFormatAddress(r.IP, r.Port)
}

type Scope struct {
	Domain    string
	Resolvers []*Resolver
}

var (
	config = configuration.Get()

	globalResolvers []*Resolver // all resolvers
	localResolvers  []*Resolver // all resolvers that are in site-local or link-local IP ranges
	localScopes     []Scope     // list of scopes with a list of local resolvers that can resolve the scope
	mDNSResolver    *Resolver   // holds a reference to the mDNS resolver
	resolversLock   sync.RWMutex

	env = environment.NewInterface()

	dupReqMap  = make(map[string]*sync.Mutex)
	dupReqLock sync.Mutex
)

func init() {
	loadResolvers(false)
}

func indexOfResolver(server string, list []*Resolver) int {
	for k, v := range list {
		if v.Server == server {
			return k
		}
	}
	return -1
}

func indexOfScope(domain string, list *[]Scope) int {
	for k, v := range *list {
		if v.Domain == domain {
			return k
		}
	}
	return -1
}

func parseAddress(server string) (*net.IP, uint16, error) {
	delimiter := strings.LastIndex(server, ":")
	if delimiter < 0 {
		return nil, 0, errors.New("port missing")
	}
	ip := net.ParseIP(strings.Trim(server[:delimiter], "[]"))
	if ip == nil {
		return nil, 0, errors.New("invalid IP address")
	}
	port, err := strconv.Atoi(server[delimiter+1:])
	if err != nil || port < 1 || port > 65536 {
		return nil, 0, errors.New("invalid port")
	}
	return &ip, uint16(port), nil
}

func urlFormatAddress(ip *net.IP, port uint16) string {
	var address string
	if ipv4 := ip.To4(); ipv4 != nil {
		address = fmt.Sprintf("%s:%d", ipv4.String(), port)
	} else {
		address = fmt.Sprintf("[%s]:%d", ip.String(), port)
	}
	return address
}

func loadResolvers(resetResolvers bool) {
	// TODO: what happens when a lot of processes want to reload at once? we do not need to run this multiple times in a short time frame.
	resolversLock.Lock()
	defer resolversLock.Unlock()

	var newResolvers []*Resolver

configuredServersLoop:
	for _, server := range config.DNSServers {
		key := indexOfResolver(server, newResolvers)
		if key >= 0 {
			continue configuredServersLoop
		}
		key = indexOfResolver(server, globalResolvers)
		if resetResolvers || key == -1 {
			parts := strings.Split(server, "|")
			if len(parts) < 2 {
				log.Warningf("intel: invalid DNS server in config: %s (invalid format)", server)
				continue configuredServersLoop
			}
			var lastFail int64
			new := &Resolver{
				Server:        server,
				ServerAddress: parts[1],
				LastFail:      &lastFail,
				Source:        "config",
				Initialized:   abool.NewBool(false),
			}
			ip, port, err := parseAddress(parts[1])
			if err != nil {
				new.IP = ip
				new.Port = port
			}
			switch {
			case strings.HasPrefix(server, "DNS|"):
				new.Resolve = queryDNS
				new.AllowedSecurityLevel = configuration.SecurityLevelFortress
			case strings.HasPrefix(server, "DoH|"):
				new.Resolve = queryDNSoverHTTPS
				new.AllowedSecurityLevel = configuration.SecurityLevelFortress
				new.SkipFqdnBeforeInit = dns.Fqdn(strings.Split(parts[1], ":")[0])

				tls := &tls.Config{
				// TODO: use custom random
				// Rand: io.Reader,
				}
				tr := &http.Transport{
					MaxIdleConnsPerHost: 100,
					TLSClientConfig:     tls,
					// TODO: use custom resolver as of Go1.9
				}
				if len(parts) == 3 && strings.HasPrefix(parts[2], "df:") {
					// activate domain fronting
					tls.ServerName = parts[2][3:]
					addDomainFronting(new.SkipFqdnBeforeInit, dns.Fqdn(tls.ServerName))
					new.SkipFqdnBeforeInit = dns.Fqdn(tls.ServerName)
				}
				new.HTTPClient = &http.Client{Transport: tr}

			default:
				log.Warningf("intel: invalid DNS server in config: %s (not starting with a valid identifier)", server)
				continue configuredServersLoop
			}
			newResolvers = append(newResolvers, new)
		} else {
			newResolvers = append(newResolvers, globalResolvers[key])
		}
	}

	// add local resolvers
	assignedNameservers := environment.Nameservers()
assignedServersLoop:
	for _, nameserver := range assignedNameservers {
		server := fmt.Sprintf("DNS|%s", urlFormatAddress(&nameserver.IP, 53))
		key := indexOfResolver(server, newResolvers)
		if key >= 0 {
			continue assignedServersLoop
		}
		key = indexOfResolver(server, globalResolvers)
		if resetResolvers || key == -1 {
			var lastFail int64
			new := &Resolver{
				Server:               server,
				ServerAddress:        urlFormatAddress(&nameserver.IP, 53),
				IP:                   &nameserver.IP,
				Port:                 53,
				LastFail:             &lastFail,
				Resolve:              queryDNS,
				AllowedSecurityLevel: configuration.SecurityLevelFortress,
				Initialized:          abool.NewBool(false),
				Source:               "dhcp",
			}
			if netutils.IPIsLocal(nameserver.IP) && len(nameserver.Search) > 0 {
				// only allow searches for local resolvers
				var newSearch []string
				for _, value := range nameserver.Search {
					newSearch = append(newSearch, fmt.Sprintf(".%s.", strings.Trim(value, ".")))
				}
				new.Search = &newSearch
			}
			newResolvers = append(newResolvers, new)
		} else {
			newResolvers = append(newResolvers, globalResolvers[key])
		}
	}

	// save resolvers
	globalResolvers = newResolvers
	if len(globalResolvers) == 0 {
		log.Criticalf("intel: no (valid) dns servers found in configuration and system")
	}

	// make list with local resolvers
	localResolvers = make([]*Resolver, 0)
	for _, resolver := range globalResolvers {
		if resolver.IP != nil && netutils.IPIsLocal(*resolver.IP) {
			localResolvers = append(localResolvers, resolver)
		}
	}

	// add resolvers to every scope the cover
	localScopes = make([]Scope, 0)
	for _, resolver := range globalResolvers {

		if resolver.Search != nil {
			// add resolver to custom searches
			for _, search := range *resolver.Search {
				if search == "." {
					continue
				}
				key := indexOfScope(search, &localScopes)
				if key == -1 {
					localScopes = append(localScopes, Scope{
						Domain:    search,
						Resolvers: []*Resolver{resolver},
					})
				} else {
					localScopes[key].Resolvers = append(localScopes[key].Resolvers, resolver)
				}
			}

		}
	}

	// init mdns resolver
	if mDNSResolver == nil {
		cannotFail := int64(-1)
		mDNSResolver = &Resolver{
			Server:               "mDNS",
			Resolve:              queryMulticastDNS,
			AllowedSecurityLevel: config.DoNotUseMDNS.Level(),
			Initialized:          abool.NewBool(false),
			Source:               "static",
			LastFail:             &cannotFail,
		}
	}

	// sort scopes by length
	sort.Slice(localScopes,
		func(i, j int) bool {
			return len(localScopes[i].Domain) > len(localScopes[j].Domain)
		},
	)

	log.Trace("intel: loaded global resolvers:")
	for _, resolver := range globalResolvers {
		log.Tracef("intel: %s", resolver.Server)
	}
	log.Trace("intel: loaded local resolvers:")
	for _, resolver := range localResolvers {
		log.Tracef("intel: %s", resolver.Server)
	}
	log.Trace("intel: loaded scopes:")
	for _, scope := range localScopes {
		var scopeServers []string
		for _, resolver := range scope.Resolvers {
			scopeServers = append(scopeServers, resolver.Server)
		}
		log.Tracef("intel: %s: %s", scope.Domain, strings.Join(scopeServers, ", "))
	}

}

// Resolve resolves the given query for a domain and type and returns a RRCache object or nil, if the query failed.
func Resolve(fqdn string, qtype dns.Type, securityLevel int8) *RRCache {
	fqdn = dns.Fqdn(fqdn)

	// use this to time how long it takes resolve this domain
	// timed := time.Now()
	// defer log.Tracef("intel: took %s to get resolve %s%s", time.Now().Sub(timed).String(), fqdn, qtype.String())

	// handle request for localhost
	if fqdn == "localhost." {
		var rr dns.RR
		var err error
		switch uint16(qtype) {
		case dns.TypeA:
			rr, err = dns.NewRR("localhost. 3600 IN A 127.0.0.1")
		case dns.TypeAAAA:
			rr, err = dns.NewRR("localhost. 3600 IN AAAA ::1")
		default:
			return nil
		}
		if err != nil {
			return nil
		}
		return &RRCache{
			Answer: []dns.RR{rr},
		}
	}

	// check cache
	rrCache, err := GetRRCache(fqdn, qtype)
	if err != nil {
		switch err {
		case database.ErrNotFound:
		default:
			log.Warningf("intel: getting RRCache %s%s from database failed: %s", fqdn, qtype.String(), err)
		}
		return resolveAndCache(fqdn, qtype, securityLevel)
	}

	if rrCache.Expires <= time.Now().Unix() {
		rrCache.requestingNew = true
		go resolveAndCache(fqdn, qtype, securityLevel)
	}

	// randomize records to allow dumb clients (who only look at the first record) to reliably connect
	for i := range rrCache.Answer {
		j := rand.Intn(i + 1)
		rrCache.Answer[i], rrCache.Answer[j] = rrCache.Answer[j], rrCache.Answer[i]
	}

	return rrCache
}

func resolveAndCache(fqdn string, qtype dns.Type, securityLevel int8) *RRCache {
	// log.Tracef("intel: resolving %s%s", fqdn, qtype.String())

	rrCache, ok := checkDomainFronting(fqdn, qtype, securityLevel)
	if ok {
		if rrCache == nil {
			return nil
		}
		return rrCache
	}

	// dedup requests
	dupKey := fmt.Sprintf("%s%s", fqdn, qtype.String())
	dupReqLock.Lock()
	mutex, requestActive := dupReqMap[dupKey]
	if !requestActive {
		mutex = new(sync.Mutex)
		mutex.Lock()
		dupReqMap[dupKey] = mutex
		dupReqLock.Unlock()
	} else {
		dupReqLock.Unlock()
		log.Tracef("intel: waiting for duplicate query for %s to complete", dupKey)
		mutex.Lock()
		// wait until duplicate request is finished, then fetch current RRCache and return
		mutex.Unlock()
		var err error
		rrCache, err = GetRRCache(dupKey, qtype)
		if err == nil {
			return rrCache
		}
		// must have been nxdomain if we cannot get RRCache
		return nil
	}
	defer func() {
		dupReqLock.Lock()
		delete(dupReqMap, fqdn)
		dupReqLock.Unlock()
		mutex.Unlock()
	}()

	// resolve
	rrCache = intelligentResolve(fqdn, qtype, securityLevel)
	if rrCache == nil {
		return nil
	}

	// persist to database
	rrCache.Clean(600)
	rrCache.CreateWithType(fqdn, qtype)

	return rrCache
}

func intelligentResolve(fqdn string, qtype dns.Type, securityLevel int8) *RRCache {

	// TODO: handle being offline
	// TODO: handle multiple network connections

	if config.Changed() {
		log.Info("intel: config changed, reloading resolvers")
		loadResolvers(false)
	} else if env.NetworkChanged() {
		log.Info("intel: network changed, reloading resolvers")
		loadResolvers(true)
	}
	config.RLock()
	defer config.RUnlock()
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	lastFailBoundary := time.Now().Unix() - config.DNSServerRetryRate
	preDottedFqdn := "." + fqdn

	// resolve:
	// reverse local -> local, mdns
	// local -> local scopes, mdns
	// special -> local scopes, local
	// global -> local scopes, global

	// local reverse scope
	if domainInScopes(preDottedFqdn, localReverseScopes) {
		// try local resolvers
		for _, resolver := range localResolvers {
			rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
			if ok && rrCache != nil && !rrCache.IsNXDomain() {
				return rrCache
			}
		}
		// check config
		if config.DoNotUseMDNS.IsSetWithLevel(securityLevel) {
			return nil
		}
		// try mdns
		rrCache, _ := tryResolver(mDNSResolver, lastFailBoundary, fqdn, qtype, securityLevel)
		return rrCache
	}

	// local scopes
	for _, scope := range localScopes {
		if strings.HasSuffix(preDottedFqdn, scope.Domain) {
			for _, resolver := range scope.Resolvers {
				rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
				if ok && rrCache != nil && !rrCache.IsNXDomain() {
					return rrCache
				}
			}
		}
	}

	switch {
	case strings.HasSuffix(preDottedFqdn, ".local."):
		// check config
		if config.DoNotUseMDNS.IsSetWithLevel(securityLevel) {
			return nil
		}
		// try mdns
		rrCache, _ := tryResolver(mDNSResolver, lastFailBoundary, fqdn, qtype, securityLevel)
		return rrCache
	case domainInScopes(preDottedFqdn, specialScopes):
		// check config
		if config.DoNotForwardSpecialDomains.IsSetWithLevel(securityLevel) {
			return nil
		}
		// try local resolvers
		for _, resolver := range localResolvers {
			rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
			if ok {
				return rrCache
			}
		}
	default:
		// try global resolvers
		for _, resolver := range globalResolvers {
			rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
			if ok {
				return rrCache
			}
		}
	}

	log.Criticalf("intel: failed to resolve %s%s: all resolvers failed (or were skipped to fulfill the security level)", fqdn, qtype.String())
	return nil

	// TODO: check if there would be resolvers available in lower security modes and alert user

}

func tryResolver(resolver *Resolver, lastFailBoundary int64, fqdn string, qtype dns.Type, securityLevel int8) (*RRCache, bool) {
	// skip if not allowed in current security level
	if resolver.AllowedSecurityLevel < config.SecurityLevel() || resolver.AllowedSecurityLevel < securityLevel {
		log.Tracef("intel: skipping resolver %s, because it isn't allowed to operate on the current security level: %d|%d", resolver, config.SecurityLevel(), securityLevel)
		return nil, false
	}
	// skip if not security level denies assigned dns servers
	if config.DoNotUseAssignedDNS.IsSetWithLevel(securityLevel) && resolver.Source == "dhcp" {
		log.Tracef("intel: skipping resolver %s, because assigned nameservers are not allowed on the current security level: %d|%d (%d)", resolver, config.SecurityLevel(), securityLevel, int8(config.DoNotUseAssignedDNS))
		return nil, false
	}
	// check if failed recently
	if atomic.LoadInt64(resolver.LastFail) > lastFailBoundary {
		return nil, false
	}
	// TODO: put SkipFqdnBeforeInit back into !resolver.Initialized.IsSet() as soon as Go1.9 arrives and we can use a custom resolver
	// skip resolver if initializing and fqdn is set to skip
	if fqdn == resolver.SkipFqdnBeforeInit {
		return nil, false
	}
	// check if resolver is already initialized
	if !resolver.Initialized.IsSet() {
		// first should init, others wait
		resolver.InitLock.Lock()
		if resolver.Initialized.IsSet() {
			// unlock immediately if resolver was initialized
			resolver.InitLock.Unlock()
		} else {
			// initialize and unlock when finished
			defer resolver.InitLock.Unlock()
		}
		// check if previous init failed
		if atomic.LoadInt64(resolver.LastFail) > lastFailBoundary {
			return nil, false
		}
	}
	// resolve
	log.Tracef("intel: trying to resolve %s%s with %s", fqdn, qtype.String(), resolver.Server)
	rrCache, err := resolver.Resolve(resolver, fqdn, qtype)
	if err != nil {
		// check if failing is disabled
		if atomic.LoadInt64(resolver.LastFail) == -1 {
			log.Tracef("intel: non-failing resolver %s failed (%s), moving to next", resolver, err)
			return nil, false
		}
		log.Warningf("intel: resolver %s failed (%s), moving to next", resolver, err)
		resolver.LockReason.Lock()
		resolver.FailReason = err.Error()
		resolver.LockReason.Unlock()
		atomic.StoreInt64(resolver.LastFail, time.Now().Unix())
		resolver.Initialized.UnSet()
		return nil, false
	}
	resolver.Initialized.SetToIf(false, true)
	return rrCache, true
}

func queryDNS(resolver *Resolver, fqdn string, qtype dns.Type) (*RRCache, error) {

	q := new(dns.Msg)
	q.SetQuestion(fqdn, uint16(qtype))

	var reply *dns.Msg
	var err error
	for i := 0; i < 5; i++ {
		client := new(dns.Client)
		reply, _, err = client.Exchange(q, resolver.ServerAddress)
		if err != nil {

			// TODO: handle special cases
			// 1. connect: network is unreachable
			// 2. timeout

			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Tracef("intel: retrying to resolve %s%s with %s, error was: %s", fqdn, qtype.String(), resolver.Server, err)
				continue
			}
			break
		}
	}

	if err != nil {
		log.Warningf("resolving %s%s failed: %s", fqdn, qtype.String(), err)
		return nil, fmt.Errorf("resolving %s%s failed: %s", fqdn, qtype.String(), err)
	}

	new := &RRCache{
		Answer: reply.Answer,
		Ns:     reply.Ns,
		Extra:  reply.Extra,
	}

	// TODO: check if reply.Answer is valid
	return new, nil
}

type DnsOverHttpsReply struct {
	Status     uint32
	Truncated  bool `json:"TC"`
	Answer     []DohRR
	Additional []DohRR
}

type DohRR struct {
	Name  string `json:"name"`
	Qtype uint16 `json:"type"`
	TTL   uint32 `json:"TTL"`
	Data  string `json:"data"`
}

func queryDNSoverHTTPS(resolver *Resolver, fqdn string, qtype dns.Type) (*RRCache, error) {

	// API documentation: https://developers.google.com/speed/public-dns/docs/dns-over-https

	payload := url.Values{}
	payload.Add("name", fqdn)
	payload.Add("type", strconv.Itoa(int(qtype)))
	payload.Add("edns_client_subnet", "0.0.0.0/0")
	// TODO: add random - only use upper- and lower-case letters, digits, hyphen, period, underscore and tilde
	// payload.Add("random_padding", "")

	resp, err := resolver.HTTPClient.Get(fmt.Sprintf("https://%s/resolve?%s", resolver.ServerAddress, payload.Encode()))
	if err != nil {
		return nil, fmt.Errorf("resolving %s%s failed: http error: %s", fqdn, qtype.String(), err)
		// TODO: handle special cases
		// 1. connect: network is unreachable
		// intel: resolver DoH|dns.google.com:443|df:www.google.com failed (resolving discovery-v4-4.syncthing.net.A failed: http error: Get https://dns.google.com:443/resolve?edns_client_subnet=0.0.0.0%2F0&name=discovery-v4-4.syncthing.net.&type=1: dial tcp [2a00:1450:4001:819::2004]:443: connect: network is unreachable), moving to next
		// 2. timeout
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("resolving %s%s failed: request was unsuccessful, got code %d", fqdn, qtype.String(), resp.StatusCode)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("resolving %s%s failed: error reading response body: %s", fqdn, qtype.String(), err)
	}

	var reply DnsOverHttpsReply
	err = json.Unmarshal(body, &reply)
	if err != nil {
		return nil, fmt.Errorf("resolving %s%s failed: error parsing response body: %s", fqdn, qtype.String(), err)
	}

	if reply.Status != 0 {
		// this happens if there is a server error (e.g. DNSSEC fail), ignore for now
		// TODO: do something more intelligent
	}

	new := new(RRCache)

	// TODO: handle TXT records

	for _, entry := range reply.Answer {
		rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", entry.Name, entry.TTL, dns.Type(entry.Qtype).String(), entry.Data))
		if err != nil {
			log.Warningf("intel: resolving %s%s failed: failed to parse record to DNS: %s %d IN %s %s", fqdn, qtype.String(), entry.Name, entry.TTL, dns.Type(entry.Qtype).String(), entry.Data)
			continue
		}
		new.Answer = append(new.Answer, rr)
	}

	for _, entry := range reply.Additional {
		rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", entry.Name, entry.TTL, dns.Type(entry.Qtype).String(), entry.Data))
		if err != nil {
			log.Warningf("intel: resolving %s%s failed: failed to parse record to DNS: %s %d IN %s %s", fqdn, qtype.String(), entry.Name, entry.TTL, dns.Type(entry.Qtype).String(), entry.Data)
			continue
		}
		new.Extra = append(new.Extra, rr)
	}

	return new, nil
}

// TODO: implement T-DNS: DNS over TCP/TLS
// server list: https://dnsprivacy.org/wiki/display/DP/DNS+Privacy+Test+Servers
