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

// IPInfo represents various information about an IP.
type IPInfo struct {
	record.Base
	sync.Mutex

	IP      string
	Domains []string
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
		return new, nil
	}

	// or adjust type
	new, ok := r.(*IPInfo)
	if !ok {
		return nil, fmt.Errorf("record not of type *IPInfo, but %T", r)
	}
	return new, nil
}

// AddDomain adds a domain to the list and reports back if it was added, or was already present.
func (ipi *IPInfo) AddDomain(domain string) (added bool) {
	ipi.Lock()
	defer ipi.Unlock()
	if !utils.StringInSlice(ipi.Domains, domain) {
		ipi.Domains = append([]string{domain}, ipi.Domains...)
		return true
	}
	return false
}

// Save saves the IPInfo record to the database.
func (ipi *IPInfo) Save() error {
	ipi.Lock()
	if !ipi.KeyIsSet() {
		ipi.SetKey(makeIPInfoKey(ipi.IP))
	}
	ipi.Unlock()
	return ipInfoDatabase.Put(ipi)
}

// FmtDomains returns a string consisting of the domains that have seen to use this IP, joined by " or "
func (ipi *IPInfo) FmtDomains() string {
	return strings.Join(ipi.Domains, " or ")
}

// FmtDomains returns a string consisting of the domains that have seen to use this IP, joined by " or "
func (ipi *IPInfo) String() string {
	ipi.Lock()
	defer ipi.Unlock()
	return fmt.Sprintf("<IPInfo[%s] %s: %s", ipi.Key(), ipi.IP, ipi.FmtDomains())
}
