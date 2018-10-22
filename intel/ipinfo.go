package intel

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/record"
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

	IP      net.IP
	Domains []string
}

func makeIPInfoKey(ip net.IP) string {
	return fmt.Sprintf("intel:IPInfo/%s", ip.String())
}

// GetIPInfo gets an IPInfo record from the database.
func GetIPInfo(ip net.IP) (*IPInfo, error) {
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

// Save saves the IPInfo record to the database.
func (ipi *IPInfo) Save() error {
	ipi.SetKey(makeIPInfoKey(ipi.IP))
	return ipInfoDatabase.PutNew(ipi)
}

// FmtDomains returns a string consisting of the domains that have seen to use this IP, joined by " or "
func (ipi *IPInfo) FmtDomains() string {
	return strings.Join(ipi.Domains, " or ")
}
