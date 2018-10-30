package profile

import "strings"

// Domains is a list of permitted or denied domains.
type Domains map[string]*DomainDecision

// DomainDecision holds a decision about a domain.
type DomainDecision struct {
	Permit            bool
	Created           int64
	IncludeSubdomains bool
}

// IsSet returns whether the Domains object is "set".
func (d Domains) IsSet() bool {
	if d != nil {
		return true
	}
	return false
}

// CheckStatus checks if the given domain is governed in the list of domains and returns whether it is permitted.
func (d Domains) CheckStatus(domain string) (permit, ok bool) {
	// check for exact domain
	dd, ok := d[domain]
	if ok {
		return dd.Permit, true
	}

	// check if domain is a subdomain of any of the domains
	for key, dd := range d {
		if dd.IncludeSubdomains && strings.HasSuffix(domain, key) {
			preDottedKey := "." + key
			if strings.HasSuffix(domain, preDottedKey) {
				return dd.Permit, true
			}
		}
	}

	return false, false
}
