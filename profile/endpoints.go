package profile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Safing/portmaster/intel"
)

// Endpoints is a list of permitted or denied endpoints.
type Endpoints []*EndpointPermission

// EndpointPermission holds a decision about an endpoint.
type EndpointPermission struct {
	DomainOrIP string
	Wildcard   bool
	Protocol   uint8
	StartPort  uint16
	EndPort    uint16
	Permit     bool
	Created    int64
}

// IsSet returns whether the Endpoints object is "set".
func (e Endpoints) IsSet() bool {
	if len(e) > 0 {
		return true
	}
	return false
}

// Check checks if the given domain is governed in the list of domains and returns whether it is permitted.
// If getDomainOfIP (returns reverse and forward dns matching domain name) is supplied, an IP will be resolved to a domain, if necessary.
func (e Endpoints) Check(domainOrIP string, protocol uint8, port uint16, checkReverseIP bool, securityLevel uint8) (permit bool, reason string, ok bool) {

	// ip resolving
	var cachedGetDomainOfIP func() string
	if checkReverseIP {
		var ipResolved bool
		var ipName string
		// setup caching wrapper
		cachedGetDomainOfIP = func() string {
			if !ipResolved {
				result, err := intel.ResolveIPAndValidate(domainOrIP, securityLevel)
				if err != nil {
					// log.Debug()
					ipName = result
				}
				ipResolved = true
			}
			return ipName
		}
	}

	isDomain := strings.HasSuffix(domainOrIP, ".")

	for _, entry := range e {
		if ok, reason := entry.Matches(domainOrIP, protocol, port, isDomain, cachedGetDomainOfIP); ok {
			return entry.Permit, reason, true
		}
	}

	return false, "", false
}

func isSubdomainOf(domain, subdomain string) bool {
	dotPrefixedDomain := "." + domain
	return strings.HasSuffix(subdomain, dotPrefixedDomain)
}

// Matches checks whether the given endpoint has a managed permission. If getDomainOfIP (returns reverse and forward dns matching domain name) is supplied, this declares an incoming connection.
func (ep EndpointPermission) Matches(domainOrIP string, protocol uint8, port uint16, isDomain bool, getDomainOfIP func() string) (match bool, reason string) {
	if ep.Protocol > 0 && protocol != ep.Protocol {
		return false, ""
	}

	if ep.StartPort > 0 && (port < ep.StartPort || port > ep.EndPort) {
		return false, ""
	}

	switch {
	case ep.Wildcard && len(ep.DomainOrIP) == 0:
		// host wildcard
		return true, fmt.Sprintf("%s matches %s", domainOrIP, ep)
	case domainOrIP == ep.DomainOrIP:
		// host match
		return true, fmt.Sprintf("%s matches %s", domainOrIP, ep)
	case isDomain && ep.Wildcard && isSubdomainOf(ep.DomainOrIP, domainOrIP):
		// subdomain match
		return true, fmt.Sprintf("%s matches %s", domainOrIP, ep)
	case !isDomain && getDomainOfIP != nil && getDomainOfIP() == ep.DomainOrIP:
		// resolved IP match
		return true, fmt.Sprintf("%s->%s matches %s", domainOrIP, getDomainOfIP(), ep)
	case !isDomain && getDomainOfIP != nil && ep.Wildcard && isSubdomainOf(ep.DomainOrIP, getDomainOfIP()):
		// resolved IP subdomain match
		return true, fmt.Sprintf("%s->%s matches %s", domainOrIP, getDomainOfIP(), ep)
	default:
		// no match
		return false, ""
	}
}

func (e Endpoints) String() string {
	var s []string
	for _, entry := range e {
		s = append(s, entry.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ", "))
}

func (ep EndpointPermission) String() string {
	s := ep.DomainOrIP

	s += " "

	if ep.Protocol > 0 {
		s += strconv.Itoa(int(ep.Protocol))
	} else {
		s += "*"
	}

	s += "/"

	if ep.StartPort > 0 {
		if ep.StartPort == ep.EndPort {
			s += strconv.Itoa(int(ep.StartPort))
		} else {
			s += fmt.Sprintf("%d-%d", ep.StartPort, ep.EndPort)
		}
	} else {
		s += "*"
	}

	return s
}
