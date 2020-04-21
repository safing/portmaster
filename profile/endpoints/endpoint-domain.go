package endpoints

import (
	"regexp"
	"strings"

	"github.com/safing/portmaster/intel"
)

const (
	domainMatchTypeExact uint8 = iota
	domainMatchTypeZone
	domainMatchTypeSuffix
	domainMatchTypePrefix
	domainMatchTypeContains
)

var (
	domainRegex    = regexp.MustCompile(`^\*?(([a-z0-9][a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z]{2,}\.?$`)
	altDomainRegex = regexp.MustCompile(`^\*?[a-z0-9\.-]+\*$`)
)

// EndpointDomain matches domains.
type EndpointDomain struct {
	EndpointBase

	OriginalValue string
	Domain        string
	DomainZone    string
	MatchType     uint8
}

func (ep *EndpointDomain) check(entity *intel.Entity, domain string) (EPResult, Reason) {
	result, reason := ep.match(ep, entity, ep.Domain, "domain matches")

	switch ep.MatchType {
	case domainMatchTypeExact:
		if domain == ep.Domain {
			return result, reason
		}
	case domainMatchTypeZone:
		if domain == ep.Domain {
			return result, reason
		}
		if strings.HasSuffix(domain, ep.DomainZone) {
			return result, reason
		}
	case domainMatchTypeSuffix:
		if strings.HasSuffix(domain, ep.Domain) {
			return result, reason
		}
	case domainMatchTypePrefix:
		if strings.HasPrefix(domain, ep.Domain) {
			return result, reason
		}
	case domainMatchTypeContains:
		if strings.Contains(domain, ep.Domain) {
			return result, reason
		}
	}
	return NoMatch, nil
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointDomain) Matches(entity *intel.Entity) (EPResult, Reason) {
	if entity.Domain == "" {
		return NoMatch, nil
	}

	result, reason := ep.check(entity, entity.Domain)
	if result != NoMatch {
		return result, reason
	}

	if entity.CNAMECheckEnabled() {
		for _, domain := range entity.CNAME {
			result, reason = ep.check(entity, domain)
			if result == Denied {
				return result, reason
			}
		}
	}

	return NoMatch, nil
}

func (ep *EndpointDomain) String() string {
	return ep.renderPPP(ep.OriginalValue)
}

func parseTypeDomain(fields []string) (Endpoint, error) {
	domain := fields[1]

	if domainRegex.MatchString(domain) || altDomainRegex.MatchString(domain) {
		ep := &EndpointDomain{
			OriginalValue: domain,
		}

		// fix domain ending
		switch domain[len(domain)-1] {
		case '.':
		case '*':
		default:
			domain += "."
		}

		// fix domain case
		domain = strings.ToLower(domain)

		switch {
		case strings.HasPrefix(domain, "*") && strings.HasSuffix(domain, "*"):
			ep.MatchType = domainMatchTypeContains
			ep.Domain = strings.Trim(domain, "*")
			return ep.parsePPP(ep, fields)

		case strings.HasSuffix(domain, "*"):
			ep.MatchType = domainMatchTypePrefix
			ep.Domain = strings.Trim(domain, "*")
			return ep.parsePPP(ep, fields)

		case strings.HasPrefix(domain, "*"):
			ep.MatchType = domainMatchTypeSuffix
			ep.Domain = strings.Trim(domain, "*")
			return ep.parsePPP(ep, fields)

		case strings.HasPrefix(domain, "."):
			ep.MatchType = domainMatchTypeZone
			ep.Domain = strings.TrimLeft(domain, ".")
			ep.DomainZone = "." + ep.Domain
			return ep.parsePPP(ep, fields)

		default:
			ep.MatchType = domainMatchTypeExact
			ep.Domain = domain
			return ep.parsePPP(ep, fields)
		}
	}

	return nil, nil
}
