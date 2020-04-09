package intel

import (
	"context"
	"net"
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/status"
)

// Entity describes a remote endpoint in many different ways.
type Entity struct {
	sync.Mutex

	Domain             string
	IP                 net.IP
	Protocol           uint8
	Port               uint16
	doReverseResolve   bool
	reverseResolveDone *abool.AtomicBool

	Country         string
	ASN             uint
	location        *geoip.Location
	locationFetched *abool.AtomicBool

	Lists        []string
	listsFetched *abool.AtomicBool
}

// Init initializes the internal state and returns the entity.
func (e *Entity) Init() *Entity {
	e.reverseResolveDone = abool.New()
	e.locationFetched = abool.New()
	e.listsFetched = abool.New()
	return e
}

// FetchData fetches additional information, meant to be called before persisting an entity record.
func (e *Entity) FetchData() {
	e.getLocation()
	e.getLists()
}

// Domain and IP

// EnableReverseResolving enables reverse resolving the domain from the IP on demand.
func (e *Entity) EnableReverseResolving() {
	e.Lock()
	defer e.Lock()

	e.doReverseResolve = true
}

func (e *Entity) reverseResolve() {
	// only get once
	if !e.reverseResolveDone.IsSet() {
		e.Lock()
		defer e.Unlock()

		// check for concurrent request
		if e.reverseResolveDone.IsSet() {
			return
		}
		defer e.reverseResolveDone.Set()

		// check if we should resolve
		if !e.doReverseResolve {
			return
		}

		// need IP!
		if e.IP == nil {
			return
		}

		// reverse resolve
		if reverseResolver == nil {
			return
		}
		// TODO: security level
		domain, err := reverseResolver(context.TODO(), e.IP.String(), status.SecurityLevelNormal)
		if err != nil {
			log.Warningf("intel: failed to resolve IP %s: %s", e.IP, err)
			return
		}
		e.Domain = domain
	}
}

// GetDomain returns the domain and whether it is set.
func (e *Entity) GetDomain() (string, bool) {
	e.reverseResolve()

	if e.Domain == "" {
		return "", false
	}
	return e.Domain, true
}

// GetIP returns the IP and whether it is set.
func (e *Entity) GetIP() (net.IP, bool) {
	if e.IP == nil {
		return nil, false
	}
	return e.IP, true
}

// Location

func (e *Entity) getLocation() {
	// only get once
	if !e.locationFetched.IsSet() {
		e.Lock()
		defer e.Unlock()

		// check for concurrent request
		if e.locationFetched.IsSet() {
			return
		}
		defer e.locationFetched.Set()

		// need IP!
		if e.IP == nil {
			log.Warningf("intel: cannot get location for %s data without IP", e.Domain)
			return
		}

		// get location data
		loc, err := geoip.GetLocation(e.IP)
		if err != nil {
			log.Warningf("intel: failed to get location data for %s: %s", e.IP, err)
			return
		}
		e.location = loc
		e.Country = loc.Country.ISOCode
		e.ASN = loc.AutonomousSystemNumber
	}
}

// GetLocation returns the raw location data and whether it is set.
func (e *Entity) GetLocation() (*geoip.Location, bool) {
	e.getLocation()

	if e.location == nil {
		return nil, false
	}
	return e.location, true
}

// GetCountry returns the two letter ISO country code and whether it is set.
func (e *Entity) GetCountry() (string, bool) {
	e.getLocation()

	if e.Country == "" {
		return "", false
	}
	return e.Country, true
}

// GetASN returns the AS number and whether it is set.
func (e *Entity) GetASN() (uint, bool) {
	e.getLocation()

	if e.ASN == 0 {
		return 0, false
	}
	return e.ASN, true
}

// Lists

func (e *Entity) getLists() {
	// only get once
	if !e.listsFetched.IsSet() {
		e.Lock()
		defer e.Unlock()

		// check for concurrent request
		if e.listsFetched.IsSet() {
			return
		}
		defer e.listsFetched.Set()

		// TODO: fetch lists
	}
}

// GetLists returns the filter list identifiers the entity matched and whether this data is set.
func (e *Entity) GetLists() ([]string, bool) {
	e.getLists()

	if e.Lists == nil {
		return nil, false
	}
	return e.Lists, true
}
