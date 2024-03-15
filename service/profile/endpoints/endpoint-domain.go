package endpoints

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/netutils"
)

const (
	domainMatchTypeExact uint8 = iota
	domainMatchTypeZone
	domainMatchTypeSuffix
	domainMatchTypePrefix
	domainMatchTypeContains
)

var (
	allowedDomainChars = regexp.MustCompile(`^[a-z0-9\.-]+$`)

	// looksLikeAnIP matches domains that look like an IP address.
	looksLikeAnIP = regexp.MustCompile(`^[0-9\.:]+$`)
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
	result, reason := ep.match(ep, entity, ep.OriginalValue, "domain matches")

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
func (ep *EndpointDomain) Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason) {
	domain, ok := entity.GetDomain(ctx, true /* mayUseReverseDomain */)
	if !ok {
		return NoMatch, nil
	}

	result, reason := ep.check(entity, domain)
	if result != NoMatch {
		return result, reason
	}

	if entity.CNAMECheckEnabled() {
		for _, cname := range entity.CNAME {
			result, reason = ep.check(entity, cname)
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
	ep := &EndpointDomain{
		OriginalValue: domain,
	}

	// Fix domain ending.
	switch domain[len(domain)-1] {
	case '.', '*':
	default:
		domain += "."
	}

	// Check if this looks like an IP address.
	// At least the TLDs has characters.
	if looksLikeAnIP.MatchString(domain) {
		return nil, nil
	}

	// Fix domain case.
	domain = strings.ToLower(domain)
	needValidFQDN := true

	switch {
	case strings.HasPrefix(domain, "*") && strings.HasSuffix(domain, "*"):
		ep.MatchType = domainMatchTypeContains
		ep.Domain = strings.TrimPrefix(domain, "*")
		ep.Domain = strings.TrimSuffix(ep.Domain, "*")
		needValidFQDN = false

	case strings.HasSuffix(domain, "*"):
		ep.MatchType = domainMatchTypePrefix
		ep.Domain = strings.TrimSuffix(domain, "*")
		needValidFQDN = false

		// Prefix matching cannot be combined with zone matching
		if strings.HasPrefix(ep.Domain, ".") {
			return nil, nil
		}

		// Do not accept domains that look like an IP address and have a suffix wildcard.
		// This is confusing, because it looks like an IP Netmask matching rule.
		if looksLikeAnIP.MatchString(ep.Domain) {
			return nil, errors.New("use CIDR notation (eg. 10.0.0.0/24) for matching ip address ranges")
		}

	case strings.HasPrefix(domain, "*"):
		ep.MatchType = domainMatchTypeSuffix
		ep.Domain = strings.TrimPrefix(domain, "*")
		needValidFQDN = false

	case strings.HasPrefix(domain, "."):
		ep.MatchType = domainMatchTypeZone
		ep.Domain = strings.TrimPrefix(domain, ".")
		ep.DomainZone = "." + ep.Domain

	default:
		ep.MatchType = domainMatchTypeExact
		ep.Domain = domain
	}

	// Validate domain "content".
	switch {
	case needValidFQDN && !netutils.IsValidFqdn(ep.Domain):
		return nil, nil
	case !needValidFQDN && !allowedDomainChars.MatchString(ep.Domain):
		return nil, nil
	case strings.Contains(ep.Domain, ".."):
		// The above regex does not catch double dots.
		return nil, nil
	}

	return ep.parsePPP(ep, fields)
}
