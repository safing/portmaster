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
	Reason        string
}

func (ep *EndpointDomain) check(entity *intel.Entity, domain string) (EPResult, string) {
	switch ep.MatchType {
	case domainMatchTypeExact:
		if domain == ep.Domain {
			return ep.matchesPPP(entity), ep.Reason
		}
	case domainMatchTypeZone:
		if domain == ep.Domain {
			return ep.matchesPPP(entity), ep.Reason
		}
		if strings.HasSuffix(domain, ep.DomainZone) {
			return ep.matchesPPP(entity), ep.Reason
		}
	case domainMatchTypeSuffix:
		if strings.HasSuffix(domain, ep.Domain) {
			return ep.matchesPPP(entity), ep.Reason
		}
	case domainMatchTypePrefix:
		if strings.HasPrefix(domain, ep.Domain) {
			return ep.matchesPPP(entity), ep.Reason
		}
	case domainMatchTypeContains:
		if strings.Contains(domain, ep.Domain) {
			return ep.matchesPPP(entity), ep.Reason
		}
	}
	return NoMatch, ""
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointDomain) Matches(entity *intel.Entity) (result EPResult, reason string) {
	if entity.Domain == "" {
		return NoMatch, ""
	}

	result, reason = ep.check(entity, entity.Domain)
	if result != NoMatch {
		return
	}

	if entity.CNAMECheckEnabled() {
		for _, domain := range entity.CNAME {
			switch ep.MatchType {
			case domainMatchTypeExact:
				if domain == ep.Domain {
					result, reason = ep.matchesPPP(entity), ep.Reason
				}
			case domainMatchTypeZone:
				if domain == ep.Domain {
					result, reason = ep.matchesPPP(entity), ep.Reason
				}
				if strings.HasSuffix(domain, ep.DomainZone) {
					result, reason = ep.matchesPPP(entity), ep.Reason
				}
			case domainMatchTypeSuffix:
				if strings.HasSuffix(domain, ep.Domain) {
					result, reason = ep.matchesPPP(entity), ep.Reason
				}
			case domainMatchTypePrefix:
				if strings.HasPrefix(domain, ep.Domain) {
					result, reason = ep.matchesPPP(entity), ep.Reason
				}
			case domainMatchTypeContains:
				if strings.Contains(domain, ep.Domain) {
					result, reason = ep.matchesPPP(entity), ep.Reason
				}
			}

			if result == Denied {
				return result, reason
			}
		}
	}

	return NoMatch, ""
}

func (ep *EndpointDomain) String() string {
	return ep.renderPPP(ep.OriginalValue)
}

func parseTypeDomain(fields []string) (Endpoint, error) {
	domain := fields[1]

	if domainRegex.MatchString(domain) || altDomainRegex.MatchString(domain) {
		ep := &EndpointDomain{
			OriginalValue: domain,
			Reason:        "domain matches " + domain,
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
