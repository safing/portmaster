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
	geoDBv4File *updater.File
	geoDBv6File *updater.File
	dbFileLock  sync.Mutex

	geoDBv4Reader *maxminddb.Reader
	geoDBv6Reader *maxminddb.Reader
	dbLock        sync.Mutex

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
		if err := openDBs(); err != nil {
			// try again the next time
			dbDoReload.SetTo(true)
			return err
		}
	}

	return nil
}

func openDBs() error {
	var err error

	geoDBv4File, err = updates.GetFile("intel/geoip/geoipv4.mmdb.gz")
	if err != nil {
		return fmt.Errorf("could not get GeoIP v4 database file: %s", err)
	}
	unpackedV4, err := geoDBv4File.Unpack(".gz", updater.UnpackGZIP)
	if err != nil {
		return err
	}
	geoDBv4Reader, err = maxminddb.Open(unpackedV4)
	if err != nil {
		return err
	}

	geoDBv6File, err = updates.GetFile("intel/geoip/geoipv6.mmdb.gz")
	if err != nil {
		return fmt.Errorf("could not get GeoIP v6 database file: %s", err)
	}
	unpackedV6, err := geoDBv6File.Unpack(".gz", updater.UnpackGZIP)
	if err != nil {
		return err
	}
	geoDBv6Reader, err = maxminddb.Open(unpackedV6)
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
	if geoDBv4Reader != nil {
		err := geoDBv4Reader.Close()
		if err != nil {
			log.Warningf("network/geoip: failed to close database: %s", err)
		}
	}
	geoDBv4Reader = nil

	if geoDBv6Reader != nil {
		err := geoDBv6Reader.Close()
		if err != nil {
			log.Warningf("network/geoip: failed to close database: %s", err)
		}
	}
	geoDBv6Reader = nil
}
