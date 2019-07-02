package geoip

import (
	"fmt"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/updates"
)

var (
	dbCity *maxminddb.Reader
	dbASN  *maxminddb.Reader

	dbLock     sync.Mutex
	dbInUse    = false // only activate if used for first time
	dbDoReload = true  // if database should be reloaded
)

func ReloadDatabases() error {
	dbLock.Lock()
	defer dbLock.Unlock()

	// don't do anything if the database isn't actually used
	if !dbInUse {
		return nil
	}

	dbDoReload = true
	return doReload()
}

func prepDatabaseForUse() error {
	dbInUse = true
	return doReload()
}

func doReload() error {
	// reload if needed
	if dbDoReload {
		defer func() {
			dbDoReload = false
		}()

		closeDBs()
		return openDBs()
	}

	return nil
}

func openDBs() error {
	var err error
	file, err := updates.GetFile("intel/geoip-city.mmdb")
	if err != nil {
		return fmt.Errorf("could not get GeoIP City database file: %s", err)
	}
	dbCity, err = maxminddb.Open(file.Path())
	if err != nil {
		return err
	}
	file, err = updates.GetFile("intel/geoip-asn.mmdb")
	if err != nil {
		return fmt.Errorf("could not get GeoIP ASN database file: %s", err)
	}
	dbASN, err = maxminddb.Open(file.Path())
	if err != nil {
		return err
	}
	return nil
}

func handleError(err error) {
	log.Warningf("network/geoip: lookup failed, reloading databases...")
	dbDoReload = true
}

func closeDBs() {
	if dbCity != nil {
		err := dbCity.Close()
		if err != nil {
			log.Warningf("network/geoip: failed to close database: %s", err)
		}
	}
	dbCity = nil

	if dbASN != nil {
		err := dbASN.Close()
		if err != nil {
			log.Warningf("network/geoip: failed to close database: %s", err)
		}
	}
	dbASN = nil
}
