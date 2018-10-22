package intel

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/Safing/portbase/log"

	"github.com/Safing/portmaster/network/environment"
	"github.com/Safing/portmaster/network/netutils"
	"github.com/Safing/portmaster/status"
)

// Resolver holds information about an active resolver.
type Resolver struct {
	// static
	Server        string
	ServerType    string
	ServerAddress string
	ServerIP      net.IP
	ServerPort    uint16
	VerifyDomain  string
	Source        string
	clientManager *clientManager

	Search               *[]string
	AllowedSecurityLevel uint8
	SkipFqdnBeforeInit   string

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

// Scope defines a domain scope and which resolvers can resolve it.
type Scope struct {
	Domain    string
	Resolvers []*Resolver
}

var (
	globalResolvers []*Resolver // all resolvers
	localResolvers  []*Resolver // all resolvers that are in site-local or link-local IP ranges
	localScopes     []*Scope    // list of scopes with a list of local resolvers that can resolve the scope
	resolversLock   sync.RWMutex

	env = environment.NewInterface()

	dupReqMap  = make(map[string]*sync.Mutex)
	dupReqLock sync.Mutex
)

func indexOfResolver(server string, list []*Resolver) int {
	for k, v := range list {
		if v.Server == server {
			return k
		}
	}
	return -1
}

func indexOfScope(domain string, list []*Scope) int {
	for k, v := range list {
		if v.Domain == domain {
			return k
		}
	}
	return -1
}

func parseAddress(server string) (net.IP, uint16, error) {
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
	return ip, uint16(port), nil
}

func urlFormatAddress(ip net.IP, port uint16) string {
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
	for _, server := range configuredNameServers() {
		key := indexOfResolver(server, newResolvers)
		if key >= 0 {
			continue configuredServersLoop
		}
		key = indexOfResolver(server, globalResolvers)
		if resetResolvers || key == -1 {

			parts := strings.Split(server, "|")
			if len(parts) < 2 {
				log.Warningf("intel: nameserver format invalid: %s", server)
				continue configuredServersLoop
			}

			ip, port, err := parseAddress(parts[1])
			if err != nil && strings.ToLower(parts[0]) != "https" {
				log.Warningf("intel: nameserver (%s) address invalid: %s", server, err)
				continue configuredServersLoop
			}

			var lastFail int64
			new := &Resolver{
				Server:        server,
				ServerType:    parts[0],
				ServerAddress: parts[1],
				ServerIP:      ip,
				ServerPort:    port,
				LastFail:      &lastFail,
				Source:        "config",
				Initialized:   abool.NewBool(false),
			}

			switch strings.ToLower(parts[0]) {
			case "dns":
				new.clientManager = newDNSClientManager(new)
			case "tcp":
				new.clientManager = newTCPClientManager(new)
			case "tls":
				new.AllowedSecurityLevel = status.SecurityLevelFortress
				if len(parts) < 3 {
					log.Warningf("intel: nameserver missing verification domain as third parameter: %s", server)
					continue configuredServersLoop
				}
				new.VerifyDomain = parts[2]
				new.clientManager = newTLSClientManager(new)
			case "https":
				new.AllowedSecurityLevel = status.SecurityLevelFortress
				new.SkipFqdnBeforeInit = dns.Fqdn(strings.Split(parts[1], ":")[0])
				if len(parts) > 2 {
					new.VerifyDomain = parts[2]
				}
				new.clientManager = newHTTPSClientManager(new)
			default:
				log.Warningf("intel: nameserver (%s) type invalid: %s", server, parts[0])
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
		server := fmt.Sprintf("dns|%s", urlFormatAddress(nameserver.IP, 53))
		key := indexOfResolver(server, newResolvers)
		if key >= 0 {
			continue assignedServersLoop
		}
		key = indexOfResolver(server, globalResolvers)
		if resetResolvers || key == -1 {

			var lastFail int64
			new := &Resolver{
				Server:               server,
				ServerType:           "dns",
				ServerAddress:        urlFormatAddress(nameserver.IP, 53),
				ServerIP:             nameserver.IP,
				ServerPort:           53,
				LastFail:             &lastFail,
				Source:               "dhcp",
				Initialized:          abool.NewBool(false),
				AllowedSecurityLevel: status.SecurityLevelSecure,
			}
			new.clientManager = newDNSClientManager(new)

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
		if resolver.ServerIP != nil && netutils.IPIsLocal(resolver.ServerIP) {
			localResolvers = append(localResolvers, resolver)
		}
	}

	// add resolvers to every scope the cover
	localScopes = make([]*Scope, 0)
	for _, resolver := range globalResolvers {

		if resolver.Search != nil {
			// add resolver to custom searches
			for _, search := range *resolver.Search {
				if search == "." {
					continue
				}
				key := indexOfScope(search, localScopes)
				if key == -1 {
					localScopes = append(localScopes, &Scope{
						Domain:    search,
						Resolvers: []*Resolver{resolver},
					})
				} else {
					localScopes[key].Resolvers = append(localScopes[key].Resolvers, resolver)
				}
			}

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
