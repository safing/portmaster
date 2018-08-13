package geoip

import (
	"net"
)

// GetLocation returns Location data of an IP address
func GetLocation(ip net.IP) (record *Location, err error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	err = prepDatabaseForUse()
	if err != nil {
		return nil, err
	}

	record = &Location{}

	// fetch
	err = dbCity.Lookup(ip, record)
	if err == nil {
		err = dbASN.Lookup(ip, record)
	}

	// retry
	if err != nil {
		// reprep
		handleError(err)
		err = prepDatabaseForUse()
		if err != nil {
			return nil, err
		}

		// refetch
		err = dbCity.Lookup(ip, record)
		if err == nil {
			err = dbASN.Lookup(ip, record)
		}
	}

	if err != nil {
		return nil, err
	}
	return record, nil
}
