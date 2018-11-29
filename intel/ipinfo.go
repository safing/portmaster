package intel

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/utils"
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
	if !utils.StringInSlice(ipi.Domains, domain) {
		newDomains := make([]string, 1, len(ipi.Domains)+1)
		newDomains[0] = domain
		ipi.Domains = append(newDomains, ipi.Domains...)
		return true
	}
	return false
}

// Save saves the IPInfo record to the database.
func (ipi *IPInfo) Save() error {
	ipi.SetKey(makeIPInfoKey(ipi.IP))
	return ipInfoDatabase.PutNew(ipi)
}

// FmtDomains returns a string consisting of the domains that have seen to use this IP, joined by " or "
func (ipi *IPInfo) FmtDomains() string {
	return strings.Join(ipi.Domains, " or ")
}
