package geoip

import (
	"fmt"
	"sync"
	"time"

	maxminddb "github.com/oschwald/maxminddb-golang"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

var worker *updateWorker

func init() {
	worker = &updateWorker{
		trigger: make(chan struct{}),
	}
}

const (
	v4MMDBResource = "intel/geoip/geoipv4.mmdb.gz"
	v6MMDBResource = "intel/geoip/geoipv6.mmdb.gz"
)

type geoIPDB struct {
	*maxminddb.Reader
	file *updater.File
}

// updateBroadcaster stores a geoIPDB and provides synchronized
// access to the MMDB reader. It also supports broadcasting to
// multiple waiters when a new database becomes available.
type updateBroadcaster struct {
	rw sync.RWMutex
	db *geoIPDB

	waiter chan struct{}
}

// NeedsUpdate returns true if the current broadcaster needs a
// database update.
func (ub *updateBroadcaster) NeedsUpdate() bool {
	ub.rw.RLock()
	defer ub.rw.RUnlock()

	return ub.db == nil || ub.db.file.UpgradeAvailable()
}

// ReplaceDatabase replaces (or initially sets) the mmdb database.
// It also notifies all waiters about the availability of the new
// database.
func (ub *updateBroadcaster) ReplaceDatabase(db *geoIPDB) {
	ub.rw.Lock()
	defer ub.rw.Unlock()

	if ub.db != nil {
		_ = ub.db.Close()
	}
	ub.db = db
	ub.notifyWaiters()
}

// notifyWaiters notifies and removes all waiters. Must be called
// with ub.rw locked.
func (ub *updateBroadcaster) notifyWaiters() {
	if ub.waiter == nil {
		return
	}
	waiter := ub.waiter
	ub.waiter = nil
	close(waiter)
}

// getWaiter appends and returns a new waiter channel that gets closed
// when a new database version is available. Must be called with
// ub.rw locked.
func (ub *updateBroadcaster) getWaiter() chan struct{} {
	if ub.waiter != nil {
		return ub.waiter
	}

	ub.waiter = make(chan struct{})
	return ub.waiter
}

type updateWorker struct {
	trigger chan struct{}
	once    sync.Once

	v4 updateBroadcaster
	v6 updateBroadcaster
}

// GetReader returns a MMDB reader for either the IPv4 or the IPv6 database.
// If wait is true GetReader will wait at most 1 second for the database to
// become available. If no database is available or GetReader times-out while
// waiting nil is returned.
func (upd *updateWorker) GetReader(v6 bool, wait bool) *maxminddb.Reader {
	// check which updateBroadcaster we need to use
	ub := &upd.v4
	if v6 {
		ub = &upd.v6
	}

	// lock the updateBroadcaster and - if we are allowed to wait -
	// create a new waiter channel, trigger an update and wait for at
	// least 1 second for the update to complete.
	ub.rw.Lock()
	if ub.db == nil {
		if wait {
			waiter := ub.getWaiter()
			ub.rw.Unlock()

			upd.triggerUpdate()

			select {
			case <-waiter:
				// call this method again but this time we don't allow
				// it to wait since there must be a open database anyway ...
				return upd.GetReader(v6, false)
			case <-time.After(time.Second):
				// we tried hard but failed so give up here
				return nil
			}
		}
		ub.rw.Unlock()
		return nil
	}
	rd := ub.db.Reader
	ub.rw.Unlock()

	return rd
}

// triggerUpdate triggers a database update check.
func (upd *updateWorker) triggerUpdate() {
	upd.start()

	select {
	case upd.trigger <- struct{}{}:
	default:
	}
}

func (upd *updateWorker) start() {
	upd.once.Do(func() {
		module.mgr.Go("geoip-updater", upd.run)
	})
}

func (upd *updateWorker) run(ctx *mgr.WorkerCtx) error {
	for {
		if upd.v4.NeedsUpdate() {
			if v4, err := getGeoIPDB(v4MMDBResource); err == nil {
				upd.v4.ReplaceDatabase(v4)
			} else {
				log.Warningf("geoip: failed to get v4 database: %s", err)
			}
		}

		if upd.v6.NeedsUpdate() {
			if v6, err := getGeoIPDB(v6MMDBResource); err == nil {
				upd.v6.ReplaceDatabase(v6)
			} else {
				log.Warningf("geoip: failed to get v6 database: %s", err)
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-upd.trigger:
		}
	}
}

func getGeoIPDB(resource string) (*geoIPDB, error) {
	log.Debugf("geoip: opening database %s", resource)

	file, unpackedPath, err := openAndUnpack(resource)
	if err != nil {
		return nil, err
	}

	reader, err := maxminddb.Open(unpackedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open: %w", err)
	}
	log.Debugf("geoip: successfully opened database %s", resource)

	return &geoIPDB{
		Reader: reader,
		file:   file,
	}, nil
}

func openAndUnpack(resource string) (*updater.File, string, error) {
	f, err := updates.GetFile(resource)
	if err != nil {
		return nil, "", fmt.Errorf("getting file: %w", err)
	}

	unpacked, err := f.Unpack(".gz", updater.UnpackGZIP)
	if err != nil {
		return nil, "", fmt.Errorf("unpacking file: %w", err)
	}

	return f, unpacked, nil
}
