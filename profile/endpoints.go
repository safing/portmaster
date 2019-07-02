package profile

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/safing/portmaster/intel"
)

// Endpoints is a list of permitted or denied endpoints.
type Endpoints []*EndpointPermission

// EndpointPermission holds a decision about an endpoint.
type EndpointPermission struct {
	Type  EPType
	Value string

	Protocol  uint8
	StartPort uint16
	EndPort   uint16

	Permit  bool
	Created int64
}

// EPType represents the type of an EndpointPermission
type EPType uint8

// EPType values
const (
	EptUnknown   EPType = 0
	EptAny       EPType = 1
	EptDomain    EPType = 2
	EptIPv4      EPType = 3
	EptIPv6      EPType = 4
	EptIPv4Range EPType = 5
	EptIPv6Range EPType = 6
	EptASN       EPType = 7
	EptCountry   EPType = 8
)

// EPResult represents the result of a check against an EndpointPermission
type EPResult uint8

// EndpointPermission return values
const (
	NoMatch EPResult = iota
	Undeterminable
	Denied
	Permitted
)

// IsSet returns whether the Endpoints object is "set".
func (e Endpoints) IsSet() bool {
	if len(e) > 0 {
		return true
	}
	return false
}

// CheckDomain checks the if the given endpoint matches a EndpointPermission in the list.
func (e Endpoints) CheckDomain(domain string) (result EPResult, reason string) {
	if domain == "" {
		return Denied, "internal error"
	}

	for _, entry := range e {
		if entry != nil {
			if result, reason = entry.MatchesDomain(domain); result != NoMatch {
				return
			}
		}
	}

	return NoMatch, ""
}

// CheckIP checks the if the given endpoint matches a EndpointPermission in the list. If _checkReverseIP_ and no domain is given, the IP will be resolved to a domain, if necessary.
func (e Endpoints) CheckIP(domain string, ip net.IP, protocol uint8, port uint16, checkReverseIP bool, securityLevel uint8) (result EPResult, reason string) {
	if ip == nil {
		return Denied, "internal error"
	}

	// ip resolving
	var cachedGetDomainOfIP func() string
	if checkReverseIP {
		var ipResolved bool
		var ipName string
		// setup caching wrapper
		cachedGetDomainOfIP = func() string {
			if !ipResolved {
				result, err := intel.ResolveIPAndValidate(ip.String(), securityLevel)
				if err != nil {
					// log.Debug()
					ipName = result
				}
				ipResolved = true
			}
			return ipName
		}
	}

	for _, entry := range e {
		if entry != nil {
			if result, reason := entry.MatchesIP(domain, ip, protocol, port, cachedGetDomainOfIP); result != NoMatch {
				return result, reason
			}
		}
	}

	return NoMatch, ""
}

func (ep EndpointPermission) matchesDomainOnly(domain string) (matches bool, reason string) {
	dotInFront := strings.HasPrefix(ep.Value, ".")
	wildcardInFront := strings.HasPrefix(ep.Value, "*")
	wildcardInBack := strings.HasSuffix(ep.Value, "*")

	switch {
	case dotInFront && !wildcardInFront && !wildcardInBack:
		// subdomain or domain
		if strings.HasSuffix(domain, ep.Value) || domain == strings.TrimPrefix(ep.Value, ".") {
			return true, fmt.Sprintf("%s matches %s", domain, ep.Value)
		}
	case wildcardInFront && wildcardInBack:
		if strings.Contains(domain, strings.Trim(ep.Value, "*")) {
			return true, fmt.Sprintf("%s matches %s", domain, ep.Value)
		}
	case wildcardInFront:
		if strings.HasSuffix(domain, strings.TrimLeft(ep.Value, "*")) {
			return true, fmt.Sprintf("%s matches %s", domain, ep.Value)
		}
	case wildcardInBack:
		if strings.HasPrefix(domain, strings.TrimRight(ep.Value, "*")) {
			return true, fmt.Sprintf("%s matches %s", domain, ep.Value)
		}
	default:
		if domain == ep.Value {
			return true, ""
		}
	}

	return false, ""
}

func (ep EndpointPermission) matchProtocolAndPortsAndReturn(protocol uint8, port uint16) (result EPResult) {
	// only check if protocol is defined
	if ep.Protocol > 0 {
		// if protocol is unknown, return Undeterminable
		if protocol == 0 {
			return Undeterminable
		}
		// if protocol does not match, return NoMatch
		if protocol != ep.Protocol {
			return NoMatch
		}
	}

	// only check if port is defined
	if ep.StartPort > 0 {
		// if port is unknown, return Undeterminable
		if port == 0 {
			return Undeterminable
		}
		// if port does not match, return NoMatch
		if port < ep.StartPort || port > ep.EndPort {
			return NoMatch
		}
	}

	// protocol and port matched or were defined as any
	if ep.Permit {
		return Permitted
	}
	return Denied
}

// MatchesDomain checks if the given endpoint matches the EndpointPermission.
func (ep EndpointPermission) MatchesDomain(domain string) (result EPResult, reason string) {
	switch ep.Type {
	case EptAny:
		// always matches
	case EptDomain:
		var matched bool
		matched, reason = ep.matchesDomainOnly(domain)
		if !matched {
			return NoMatch, ""
		}
	case EptIPv4:
		return Undeterminable, ""
	case EptIPv6:
		return Undeterminable, ""
	case EptIPv4Range:
		return Undeterminable, ""
	case EptIPv6Range:
		return Undeterminable, ""
	case EptASN:
		return Undeterminable, ""
	case EptCountry:
		return Undeterminable, ""
	default:
		return Denied, "encountered unknown enpoint permission type"
	}

	return ep.matchProtocolAndPortsAndReturn(0, 0), reason
}

// MatchesIP checks if the given endpoint matches the EndpointPermission. _getDomainOfIP_, if given, will be used to get the domain if not given.
func (ep EndpointPermission) MatchesIP(domain string, ip net.IP, protocol uint8, port uint16, getDomainOfIP func() string) (result EPResult, reason string) {
	switch ep.Type {
	case EptAny:
		// always matches
	case EptDomain:
		if domain == "" {
			if getDomainOfIP == nil {
				return NoMatch, ""
			}
			domain = getDomainOfIP()
		}

		var matched bool
		matched, reason = ep.matchesDomainOnly(domain)
		if !matched {
			return NoMatch, ""
		}
	case EptIPv4, EptIPv6:
		if ep.Value != ip.String() {
			return NoMatch, ""
		}
	case EptIPv4Range:
		return Denied, "endpoint type IP Range not yet implemented"
	case EptIPv6Range:
		return Denied, "endpoint type IP Range not yet implemented"
	case EptASN:
		return Denied, "endpoint type ASN not yet implemented"
	case EptCountry:
		return Denied, "endpoint type country not yet implemented"
	default:
		return Denied, "encountered unknown enpoint permission type"
	}

	return ep.matchProtocolAndPortsAndReturn(protocol, port), reason
}

func (e Endpoints) String() string {
	var s []string
	for _, entry := range e {
		s = append(s, entry.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ", "))
}

func (ept EPType) String() string {
	switch ept {
	case EptAny:
		return "Any"
	case EptDomain:
		return "Domain"
	case EptIPv4:
		return "IPv4"
	case EptIPv6:
		return "IPv6"
	case EptIPv4Range:
		return "IPv4-Range"
	case EptIPv6Range:
		return "IPv6-Range"
	case EptASN:
		return "ASN"
	case EptCountry:
		return "Country"
	default:
		return "Unknown"
	}
}

func (ep EndpointPermission) String() string {
	s := ep.Type.String()

	if ep.Type != EptAny {
		s += ":"
		s += ep.Value
	}
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

func (epr EPResult) String() string {
	switch epr {
	case NoMatch:
		return "No Match"
	case Undeterminable:
		return "Undeterminable"
	case Denied:
		return "Denied"
	case Permitted:
		return "Permitted"
	default:
		return "Unknown"
	}
}
