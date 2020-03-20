package resolver

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/environment"
	"github.com/safing/portmaster/network/netutils"
)

// Scope defines a domain scope and which resolvers can resolve it.
type Scope struct {
	Domain    string
	Resolvers []*Resolver
}

var (
	globalResolvers []*Resolver          // all (global) resolvers
	localResolvers  []*Resolver          // all resolvers that are in site-local or link-local IP ranges
	localScopes     []*Scope             // list of scopes with a list of local resolvers that can resolve the scope
	allResolvers    map[string]*Resolver // lookup map of all resolvers
	resolversLock   sync.RWMutex

	dupReqMap  = make(map[string]*sync.WaitGroup)
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

func getResolverByIDWithLocking(server string) *Resolver {
	resolversLock.Lock()
	defer resolversLock.Unlock()

	resolver, ok := allResolvers[server]
	if ok {
		return resolver
	}
	return nil
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

//nolint:gocyclo,gocognit
func loadResolvers() {
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
		if key == -1 {

			parts := strings.Split(server, "|")
			if len(parts) < 2 {
				log.Warningf("intel: nameserver format invalid: %s", server)
				continue configuredServersLoop
			}

			var ipScope int8
			ip, port, err := parseAddress(parts[1])
			if err == nil {
				ipScope = netutils.ClassifyIP(ip)
				if ipScope == netutils.HostLocal {
					log.Warningf(`intel: cannot use configured localhost nameserver "%s"`, server)
					continue configuredServersLoop
				}
			} else {
				if strings.ToLower(parts[0]) == "doh" {
					ipScope = netutils.Global
				} else {
					log.Warningf("intel: nameserver (%s) address invalid: %s", server, err)
					continue configuredServersLoop
				}
			}

			// create new structs
			newConn := &BasicResolverConn{}
			new := &Resolver{
				Server:        server,
				ServerType:    strings.ToLower(parts[0]),
				ServerAddress: parts[1],
				ServerIP:      ip,
				ServerIPScope: ipScope,
				ServerPort:    port,
				Source:        "config",
				Conn:          newConn,
			}

			// refer back
			newConn.resolver = new

			switch new.ServerType {
			case "dns":
				newConn.clientManager = newDNSClientManager(new)
			case "tcp":
				newConn.clientManager = newTCPClientManager(new)
			case "dot":
				if len(parts) < 3 {
					log.Warningf("intel: nameserver missing verification domain as third parameter: %s", server)
					continue configuredServersLoop
				}
				new.VerifyDomain = parts[2]
				newConn.clientManager = newTLSClientManager(new)
			case "doh":
				new.SkipFQDN = dns.Fqdn(strings.Split(parts[1], ":")[0])
				if len(parts) > 2 {
					new.VerifyDomain = parts[2]
				}
				newConn.clientManager = newHTTPSClientManager(new)
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
		if key == -1 {

			ipScope := netutils.ClassifyIP(nameserver.IP)
			if ipScope == netutils.HostLocal {
				log.Infof(`intel: cannot use assigned localhost nameserver at %s`, nameserver.IP)
				continue assignedServersLoop
			}

			// create new structs
			newConn := &BasicResolverConn{}
			new := &Resolver{
				Server:        server,
				ServerType:    "dns",
				ServerAddress: urlFormatAddress(nameserver.IP, 53),
				ServerIP:      nameserver.IP,
				ServerIPScope: ipScope,
				ServerPort:    53,
				Source:        "dhcp",
				Conn:          newConn,
			}

			// refer back
			newConn.resolver = new

			// add client manager
			newConn.clientManager = newDNSClientManager(new)

			if netutils.IPIsLAN(nameserver.IP) && len(nameserver.Search) > 0 {
				// only allow searches for local resolvers
				for _, value := range nameserver.Search {
					trimmedDomain := strings.Trim(value, ".")
					if checkSearchScope(trimmedDomain) {
						new.Search = append(new.Search, fmt.Sprintf(".%s.", strings.Trim(value, ".")))
					}
				}
				// cap to mitigate exploitation via malicious local resolver
				if len(new.Search) > 100 {
					new.Search = new.Search[:100]
				}
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
		if resolver.ServerIP != nil && netutils.IPIsLAN(resolver.ServerIP) {
			localResolvers = append(localResolvers, resolver)
		}
	}

	// add resolvers to every scope the cover
	localScopes = make([]*Scope, 0)
	for _, resolver := range globalResolvers {

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

	// log global resolvers
	if len(globalResolvers) > 0 {
		log.Trace("intel: loaded global resolvers:")
		for _, resolver := range globalResolvers {
			log.Tracef("intel: %s", resolver.Server)
		}
	} else {
		log.Warning("intel: no global resolvers loaded")
	}

	// log local resolvers
	if len(localResolvers) > 0 {
		log.Trace("intel: loaded local resolvers:")
		for _, resolver := range localResolvers {
			log.Tracef("intel: %s", resolver.Server)
		}
	} else {
		log.Info("intel: no local resolvers loaded")
	}

	// log scopes
	if len(localScopes) > 0 {
		log.Trace("intel: loaded scopes:")
		for _, scope := range localScopes {
			var scopeServers []string
			for _, resolver := range scope.Resolvers {
				scopeServers = append(scopeServers, resolver.Server)
			}
			log.Tracef("intel: %s: %s", scope.Domain, strings.Join(scopeServers, ", "))
		}
	} else {
		log.Info("intel: no scopes loaded")
	}

	// alert if no resolvers are loaded
	if len(globalResolvers) == 0 && len(localResolvers) == 0 {
		log.Critical("intel: no resolvers loaded!")
	}
}

func checkSearchScope(searchDomain string) (ok bool) {
	// sanity check
	if len(searchDomain) == 0 ||
		searchDomain[0] == '.' ||
		searchDomain[len(searchDomain)-1] == '.' {
		return false
	}

	// add more subdomains to use official publicsuffix package for our cause
	searchDomain = "*.*.*.*.*." + searchDomain

	// get suffix
	suffix, icann := publicsuffix.PublicSuffix(searchDomain)
	// sanity check
	if len(suffix) == 0 {
		return false
	}
	// inexistent (custom) tlds are okay
	// this will include special service domains! (.onion, .bit, ...)
	if !icann && !strings.Contains(suffix, ".") {
		return true
	}

	// check if suffix is a special service domain (may be handled fully by local nameserver)
	if domainInScope("."+suffix+".", specialServiceScopes) {
		return true
	}

	// build eTLD+1
	split := len(searchDomain) - len(suffix) - 1
	eTLDplus1 := searchDomain[1+strings.LastIndex(searchDomain[:split], "."):]

	// scope check
	//nolint:gosimple // want comment
	if strings.Contains(eTLDplus1, "*") {
		// oops, search domain is too high up the hierarchy
		return false
	}

	return true
}
