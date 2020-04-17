package intel

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/status"
	"golang.org/x/net/publicsuffix"
)

// Entity describes a remote endpoint in many different ways.
// It embeddes a sync.Mutex but none of the endpoints own
// functions performs locking. The caller MUST ENSURE
// proper locking and synchronization when accessing
// any properties of Entity.
type Entity struct {
	sync.Mutex

	// lists exist for most entity information and
	// we need to know which one we loaded
	domainListLoaded      bool
	ipListLoaded          bool
	countryListLoaded     bool
	asnListLoaded         bool
	reverseResolveEnabled bool
	resolveSubDomainLists bool

	Protocol uint8
	Port     uint16
	Domain   string
	IP       net.IP

	Country  string
	ASN      uint
	location *geoip.Location

	Lists    []string
	ListsMap filterlists.LookupMap

	// we only load each data above at most once
	fetchLocationOnce  sync.Once
	reverseResolveOnce sync.Once
	loadDomainListOnce sync.Once
	loadIPListOnce     sync.Once
	loadCoutryListOnce sync.Once
	loadAsnListOnce    sync.Once
}

// Init initializes the internal state and returns the entity.
func (e *Entity) Init() *Entity {
	// for backwards compatibility, remove that one
	return e
}

// FetchData fetches additional information, meant to be called before persisting an entity record.
func (e *Entity) FetchData() {
	e.getLocation()
	e.getLists()
}

// ResetLists resets the current list data and forces
// all list sources to be re-acquired when calling GetLists().
func (e *Entity) ResetLists() {
	// TODO(ppacher): our actual goal is to reset the domain
	// list right now so we could be more efficient by keeping
	// the other lists around.
	e.Lists = nil
	e.ListsMap = nil
	e.domainListLoaded = false
	e.ipListLoaded = false
	e.countryListLoaded = false
	e.asnListLoaded = false
	e.resolveSubDomainLists = false
	e.loadDomainListOnce = sync.Once{}
	e.loadIPListOnce = sync.Once{}
	e.loadCoutryListOnce = sync.Once{}
	e.loadAsnListOnce = sync.Once{}
}

// ResolveSubDomainLists enables or disables list lookups for
// sub-domains.
func (e *Entity) ResolveSubDomainLists(enabled bool) {
	if e.domainListLoaded {
		log.Warningf("intel/filterlists: tried to change sub-domain resolving for %s but lists are already fetched", e.Domain)
	}
	e.resolveSubDomainLists = enabled
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
		var domains = []string{domain}
		if e.resolveSubDomainLists {
			domains = splitDomain(domain)
			log.Debugf("intel: subdomain list resolving is enabled, checking %v", domains)
		}

		for _, d := range domains {
			log.Debugf("intel: loading domain list for %s", d)
			list, err := filterlists.LookupDomain(d)
			if err != nil {
				log.Errorf("intel: failed to get domain blocklists for %s: %s", d, err)
				e.loadDomainListOnce = sync.Once{}
				return
			}

			e.mergeList(list)
		}
		e.domainListLoaded = true
	})
}

func splitDomain(domain string) []string {
	domain = strings.Trim(domain, ".")
	suffix, _ := publicsuffix.PublicSuffix(domain)
	if suffix == domain {
		return []string{domain}
	}

	domainWithoutSuffix := domain[:len(domain)-len(suffix)]
	domainWithoutSuffix = strings.Trim(domainWithoutSuffix, ".")

	splitted := strings.FieldsFunc(domainWithoutSuffix, func(r rune) bool {
		return r == '.'
	})

	domains := make([]string, 0, len(splitted))
	for idx := range splitted {

		d := strings.Join(splitted[idx:], ".") + "." + suffix
		if d[len(d)-1] != '.' {
			d += "."
		}
		domains = append(domains, d)
	}
	return domains
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
