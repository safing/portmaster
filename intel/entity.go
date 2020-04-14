package intel

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/status"
)

// Entity describes a remote endpoint in many different ways.
// It embeddes a sync.Mutex but none of the endpoints own
// functions performs locking. The caller MUST ENSURE
// proper locking and synchronization when accessing
// any properties of Entity.
type Entity struct {
	sync.Mutex

	Domain                string
	IP                    net.IP
	Protocol              uint8
	Port                  uint16
	reverseResolveEnabled bool
	reverseResolveOnce    sync.Once

	Country           string
	ASN               uint
	location          *geoip.Location
	fetchLocationOnce sync.Once

	Lists    []string
	ListsMap filterlists.LookupMap

	// we only load each data above at most once
	loadDomainListOnce sync.Once
	loadIPListOnce     sync.Once
	loadCoutryListOnce sync.Once
	loadAsnListOnce    sync.Once

	// lists exist for most entity information and
	// we need to know which one we loaded
	domainListLoaded  bool
	ipListLoaded      bool
	countryListLoaded bool
	asnListLoaded     bool
}

// Init initializes the internal state and returns the entity.
func (e *Entity) Init() *Entity {
	// for backwards compatibility, remove that one
	return e
}

// MergeDomain copies the Domain from other to e. It does
// not lock e or other so the caller must ensure
// proper locking of entities.
func (e *Entity) MergeDomain(other *Entity) *Entity {

	// FIXME(ppacher): should we disable reverse lookups now?
	e.Domain = other.Domain

	return e
}

// MergeLists merges the intel lists stored in other with the
// lists stored in e. Neither e nor other are locked so the
// caller must ensure proper locking on both entities.
// MergeLists ensures list entries are unique and sorted.
func (e *Entity) MergeLists(other *Entity) *Entity {
	e.Lists = mergeStringList(e.Lists, other.Lists)
	e.ListsMap = buildLookupMap(e.Lists)

	// mark every list other has loaded also as
	// loaded in e. Don't copy values of lists
	// not loaded in other because they might have
	// been loaded in e.

	if other.domainListLoaded {
		e.domainListLoaded = true
	}
	if other.ipListLoaded {
		e.ipListLoaded = true
	}
	if other.countryListLoaded {
		e.countryListLoaded = true
	}
	if other.asnListLoaded {
		e.asnListLoaded = true
	}

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
	e.reverseResolveEnabled = true
}

func (e *Entity) reverseResolve() {
	e.reverseResolveOnce.Do(func() {
		// check if we should resolve
		if !e.reverseResolveEnabled {
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
	})
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
	e.fetchLocationOnce.Do(func() {
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
	})
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
	e.getDomainLists()
	e.getASNLists()
	e.getIPLists()
	e.getCountryLists()
}

func (e *Entity) mergeList(list []string) {
	e.Lists = mergeStringList(e.Lists, list)
	e.ListsMap = buildLookupMap(e.Lists)
}

func (e *Entity) getDomainLists() {
	if e.domainListLoaded {
		return
	}

	domain, ok := e.GetDomain()
	if !ok {
		return
	}

	e.loadDomainListOnce.Do(func() {
		log.Debugf("intel: loading domain list for %s", domain)
		list, err := filterlists.LookupDomain(domain)
		if err != nil {
			log.Errorf("intel: failed to get domain blocklists for %s: %s", domain, err)
			e.loadDomainListOnce = sync.Once{}
			return
		}

		e.domainListLoaded = true
		e.mergeList(list)
	})
}

func (e *Entity) getASNLists() {
	if e.asnListLoaded {
		return
	}

	asn, ok := e.GetASN()
	if !ok {
		return
	}

	log.Debugf("intel: loading ASN list for %d", asn)
	e.loadAsnListOnce.Do(func() {
		list, err := filterlists.LookupASNString(fmt.Sprintf("%d", asn))
		if err != nil {
			log.Errorf("intel: failed to get ASN blocklist for %d: %s", asn, err)
			e.loadAsnListOnce = sync.Once{}
			return
		}

		e.asnListLoaded = true
		e.mergeList(list)
	})
}

func (e *Entity) getCountryLists() {
	if e.countryListLoaded {
		return
	}

	country, ok := e.GetCountry()
	if !ok {
		return
	}

	log.Debugf("intel: loading country list for %s", country)
	e.loadCoutryListOnce.Do(func() {
		list, err := filterlists.LookupCountry(country)
		if err != nil {
			log.Errorf("intel: failed to load country blocklist for %s: %s", country, err)
			e.loadCoutryListOnce = sync.Once{}
			return
		}

		e.countryListLoaded = true
		e.mergeList(list)
	})
}

func (e *Entity) getIPLists() {
	if e.ipListLoaded {
		return
	}

	ip, ok := e.GetIP()
	if !ok {
		return
	}

	if ip == nil {
		return
	}

	// only load lists for IP addresses that are classified as global.
	if netutils.ClassifyIP(ip) != netutils.Global {
		return
	}

	log.Debugf("intel: loading IP list for %s", ip)
	e.loadIPListOnce.Do(func() {
		list, err := filterlists.LookupIP(ip)

		if err != nil {
			log.Errorf("intel: failed to get IP blocklist for %s: %s", ip.String(), err)
			e.loadIPListOnce = sync.Once{}
			return
		}
		e.ipListLoaded = true
		e.mergeList(list)
	})
}

// GetLists returns the filter list identifiers the entity matched and whether this data is set.
func (e *Entity) GetLists() ([]string, bool) {
	e.getLists()

	if e.Lists == nil {
		return nil, false
	}
	return e.Lists, true
}

// GetListsMap is like GetLists but returns a lookup map for list IDs.
func (e *Entity) GetListsMap() (filterlists.LookupMap, bool) {
	e.getLists()

	if e.ListsMap == nil {
		return nil, false
	}
	return e.ListsMap, true
}

func mergeStringList(a, b []string) []string {
	listMap := make(map[string]struct{})
	for _, s := range a {
		listMap[s] = struct{}{}
	}
	for _, s := range b {
		listMap[s] = struct{}{}
	}

	res := make([]string, 0, len(listMap))
	for s := range listMap {
		res = append(res, s)
	}
	sort.Strings(res)
	return res
}

func buildLookupMap(l []string) filterlists.LookupMap {
	m := make(filterlists.LookupMap, len(l))

	for _, s := range l {
		m[s] = struct{}{}
	}

	return m
}
