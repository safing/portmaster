package geoip

import (
	"errors"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"

	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/update"
)

var (
	dbCity *maxminddb.Reader
	dbASN  *maxminddb.Reader

	dbLock     sync.Mutex
	dbInUse    = false // only activate if used for first time
	dbDoReload = true  // if database should be reloaded

	// mmdbCityFile = "/opt/safing/GeoLite2-City.mmdb"
	// mmdbASNFile  = "/opt/safing/GeoLite2-ASN.mmdb"
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
	filepath := update.GetGeoIPCityPath()
	if filepath == "" {
		return errors.New("could not get GeoIP City filepath")
	}
	dbCity, err = maxminddb.Open(filepath)
	if err != nil {
		return err
	}
	filepath = update.GetGeoIPASNPath()
	if filepath == "" {
		return errors.New("could not get GeoIP ASN filepath")
	}
	dbASN, err = maxminddb.Open(filepath)
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
