package resolver

import (
	"fmt"
	"strings"
	"sync"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/utils"
)

var (
	ipInfoDatabase = database.NewInterface(&database.Options{
		AlwaysSetRelativateExpiry: 86400, // 24 hours
	})
)

// ResolvedDomain holds a Domain name and a list of
// CNAMES that have been resolved.
type ResolvedDomain struct {
	// Domain is the domain as requested by the application.
	Domain string

	// CNAMEs is a list of CNAMEs that have been resolved for
	// Domain.
	CNAMEs []string
}

// String returns a string representation of ResolvedDomain including
// the CNAME chain. It implements fmt.Stringer
func (resolved *ResolvedDomain) String() string {
	ret := resolved.Domain
	cnames := ""

	if len(resolved.CNAMEs) > 0 {
		cnames = " (-> " + strings.Join(resolved.CNAMEs, "->") + ")"
	}

	return ret + cnames
}

// ResolvedDomains is a helper type for operating on a slice
// of ResolvedDomain
type ResolvedDomains []ResolvedDomain

// String returns a string representation of all domains joined
// to a single string.
func (rds ResolvedDomains) String() string {
	var domains []string
	for _, n := range rds {
		domains = append(domains, n.String())
	}
	return strings.Join(domains, " or ")
}

// MostRecentDomain returns the most recent domain.
func (rds ResolvedDomains) MostRecentDomain() *ResolvedDomain {
	if len(rds) == 0 {
		return nil
	}
	// TODO(ppacher): we could also do that by using ResolvedAt()
	mostRecent := rds[len(rds)-1]
	return &mostRecent
}

// IPInfo represents various information about an IP.
type IPInfo struct {
	record.Base
	sync.Mutex

	// IP holds the acutal IP address.
	IP string

	// Domains holds a list of domains that have been
	// resolved to IP. This field is deprecated and should
	// be removed.
	// DEPRECATED: remove with alpha.
	Domains []string `json:"Domains,omitempty"`

	// ResolvedDomain is a slice of domains that
	// have been requested by various applications
	// and have been resolved to IP.
	ResolvedDomains ResolvedDomains
}

// AddDomain adds a new resolved domain to ipi.
func (ipi *IPInfo) AddDomain(resolved ResolvedDomain) bool {
	for idx, d := range ipi.ResolvedDomains {
		if d.Domain == resolved.Domain {
			if utils.StringSliceEqual(d.CNAMEs, resolved.CNAMEs) {
				return false
			}

			// we have a different CNAME chain now, remove the previous
			// entry and add it at the end.
			ipi.ResolvedDomains = append(ipi.ResolvedDomains[:idx], ipi.ResolvedDomains[idx+1:]...)
			ipi.ResolvedDomains = append(ipi.ResolvedDomains, resolved)
			return true
		}
	}

	ipi.ResolvedDomains = append(ipi.ResolvedDomains, resolved)
	return true
}

func makeIPInfoKey(ip string) string {
	return fmt.Sprintf("cache:intel/ipInfo/%s", ip)
}

// GetIPInfo gets an IPInfo record from the database.
func GetIPInfo(ip string) (*IPInfo, error) {
	key := makeIPInfoKey(ip)

	r, err := ipInfoDatabase.Get(key)
	if err != nil {
		return nil, err
	}

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &IPInfo{}
		err = record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}

		// Legacy support,
		// DEPRECATED: remove with alpha
		if len(new.Domains) > 0 && len(new.ResolvedDomains) == 0 {
			for _, d := range new.Domains {
				new.ResolvedDomains = append(new.ResolvedDomains, ResolvedDomain{
					Domain: d,
					// rest is empty...
				})
			}
			new.Domains = nil // clean up so we remove it from the database
		}

		return new, nil
	}

	// or adjust type
	new, ok := r.(*IPInfo)
	if !ok {
		return nil, fmt.Errorf("record not of type *IPInfo, but %T", r)
	}
	return new, nil
}

// Save saves the IPInfo record to the database.
func (ipi *IPInfo) Save() error {
	ipi.Lock()
	if !ipi.KeyIsSet() {
		ipi.SetKey(makeIPInfoKey(ipi.IP))
	}
	ipi.Unlock()

	// Legacy support
	// Ensure we don't write new Domain fields into the
	// database.
	// DEPRECATED: remove with alpha
	if len(ipi.Domains) > 0 {
		ipi.Domains = nil
	}

	return ipInfoDatabase.Put(ipi)
}

// FmtDomains returns a string consisting of the domains that have seen to use this IP, joined by " or "
func (ipi *IPInfo) String() string {
	ipi.Lock()
	defer ipi.Unlock()
	return fmt.Sprintf("<IPInfo[%s] %s: %s", ipi.Key(), ipi.IP, ipi.ResolvedDomains.String())
}
