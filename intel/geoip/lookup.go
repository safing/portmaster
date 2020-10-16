package geoip

import (
	"net"

	"github.com/oschwald/maxminddb-golang"
)

func getReader(ip net.IP) *maxminddb.Reader {
	if v4 := ip.To4(); v4 != nil {
		return geoDBv4Reader
	}
	return geoDBv6Reader
}

// GetLocation returns Location data of an IP address
func GetLocation(ip net.IP) (record *Location, err error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	err = prepDatabaseForUse()
	if err != nil {
		return nil, err
	}

	db := getReader(ip)

	record = &Location{}

	// fetch
	err = db.Lookup(ip, record)

	// retry
	if err != nil {
		// reprep
		handleError(err)
		err = prepDatabaseForUse()
		if err != nil {
			return nil, err
		}
		db = getReader(ip)

		// refetch
		err = db.Lookup(ip, record)
	}

	if err != nil {
		return nil, err
	}

	return record, nil
}
