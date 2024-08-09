package filterlists

import (
	"errors"
	"net"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
)

// lookupBlockLists loads the entity record for key from
// cache and returns the list of blocklist sources the
// key is part of. It is not considered an error if
// key does not exist, instead, an empty slice is
// returned.
func lookupBlockLists(entity, value string) ([]string, error) {
	key := makeListCacheKey(entity, value)
	if !isLoaded() {
		log.Warningf("intel/filterlists: not searching for %s because filterlists not loaded", key)
		// filterLists have not yet been loaded so
		// there's no point querying into the cache
		// database.
		return nil, nil
	}

	filterListLock.RLock()
	defer filterListLock.RUnlock()

	if !defaultFilter.test(entity, value) {
		return nil, nil
	}

	// log.Debugf("intel/filterlists: searching for entries with %s", key)
	entry, err := getEntityRecordByKey(key)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, nil
		}
		log.Errorf("intel/filterlists: failed to get entries for key %s: %s", key, err)

		return nil, err
	}

	return entry.Sources, nil
}

// LookupCountry returns a list of sources that mark the country
// as blocked. If country is not stored in the cache database
// a nil slice is returned.
func LookupCountry(country string) ([]string, error) {
	return lookupBlockLists("country", country)
}

// LookupDomain returns a list of sources that mark the domain
// as blocked. If domain is not stored in the cache database
// a nil slice is returned. The caller is responsible for making
// sure that the given domain is valid and canonical.
func LookupDomain(domain string) ([]string, error) {
	switch domain {
	case "", ".":
		// Return no lists for empty domains and the root zone.
		return nil, nil
	default:
		return lookupBlockLists("domain", domain)
	}
}

// LookupASNString returns a list of sources that mark the ASN
// as blocked. If ASN is not stored in the cache database
// a nil slice is returned.
func LookupASNString(asn string) ([]string, error) {
	return lookupBlockLists("asn", asn)
}

// LookupIP returns a list of block sources that contain
// a reference to ip. LookupIP automatically checks the IPv4 or
// IPv6 lists respectively.
func LookupIP(ip net.IP) ([]string, error) {
	if ip.To4() == nil {
		return LookupIPv6(ip)
	}

	return LookupIPv4(ip)
}

// LookupIPString is like LookupIP but accepts an IPv4 or
// IPv6 address in their string representations.
func LookupIPString(ipStr string) ([]string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, errors.New("invalid IP")
	}

	return LookupIP(ip)
}

// LookupIPv4String returns a list of block sources that
// contain a reference to ip. If the IP is not stored in the
// cache database a nil slice is returned.
func LookupIPv4String(ipv4 string) ([]string, error) {
	return lookupBlockLists("ipv4", ipv4)
}

// LookupIPv4 is like LookupIPv4String but accepts a net.IP.
func LookupIPv4(ipv4 net.IP) ([]string, error) {
	ip := ipv4.To4()
	if ip == nil {
		return nil, errors.New("invalid IPv4")
	}

	return LookupIPv4String(ip.String())
}

// LookupIPv6String returns a list of block sources that
// contain a reference to ip. If the IP is not stored in the
// cache database a nil slice is returned.
func LookupIPv6String(ipv6 string) ([]string, error) {
	return lookupBlockLists("ipv6", ipv6)
}

// LookupIPv6 is like LookupIPv6String but accepts a net.IP.
func LookupIPv6(ipv6 net.IP) ([]string, error) {
	ip := ipv6.To16()
	if ip == nil {
		return nil, errors.New("invalid IPv6")
	}

	return LookupIPv6String(ip.String())
}
