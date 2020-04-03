package geoip

import (
	"fmt"
	"sync"

	"github.com/tevino/abool"

	maxminddb "github.com/oschwald/maxminddb-golang"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates"
)

var (
	dbCityFile *updater.File
	dbASNFile  *updater.File
	dbFileLock sync.Mutex

	dbCity *maxminddb.Reader
	dbASN  *maxminddb.Reader
	dbLock sync.Mutex

	dbInUse    = abool.NewBool(false) // only activate if used for first time
	dbDoReload = abool.NewBool(true)  // if database should be reloaded
)

// ReloadDatabases reloads the geoip database, if they are in use.
func ReloadDatabases() error {
	// don't do anything if the database isn't actually used
	if !dbInUse.IsSet() {
		return nil
	}

	dbFileLock.Lock()
	defer dbFileLock.Unlock()
	dbLock.Lock()
	defer dbLock.Unlock()

	dbDoReload.Set()
	return doReload()
}

func prepDatabaseForUse() error {
	dbInUse.Set()
	return doReload()
}

func doReload() error {
	// reload if needed
	if dbDoReload.SetToIf(true, false) {
		closeDBs()
		return openDBs()
	}

	return nil
}

func openDBs() error {
	var err error
	dbCityFile, err = updates.GetFile("intel/geoip/geoip-city.mmdb")
	if err != nil {
		return fmt.Errorf("could not get GeoIP City database file: %s", err)
	}
	dbCity, err = maxminddb.Open(dbCityFile.Path())
	if err != nil {
		return err
	}

	dbASNFile, err = updates.GetFile("intel/geoip/geoip-asn.mmdb")
	if err != nil {
		return fmt.Errorf("could not get GeoIP ASN database file: %s", err)
	}
	dbASN, err = maxminddb.Open(dbASNFile.Path())
	if err != nil {
		return err
	}
	return nil
}

func handleError(err error) {
	log.Errorf("network/geoip: lookup failed, reloading databases: %s", err)
	dbDoReload.Set()
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
