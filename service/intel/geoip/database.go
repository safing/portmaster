package geoip

import (
	"fmt"
	"sync"
	"time"

	maxminddb "github.com/oschwald/maxminddb-golang"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

var worker *updateWorker

func init() {
	worker = &updateWorker{
		trigger: make(chan struct{}),
		v4: updateBroadcaster{
			dbName: v4MMDBResource,
		},
		v6: updateBroadcaster{
			dbName: v6MMDBResource,
		},
	}
}

const (
	v4MMDBResource = "geoipv4.mmdb"
	v6MMDBResource = "geoipv6.mmdb"
)

type geoIPDB struct {
	*maxminddb.Reader
	update *updates.Artifact
}

// updateBroadcaster stores a geoIPDB and provides synchronized
// access to the MMDB reader. It also supports broadcasting to
// multiple waiters when a new database becomes available.
type updateBroadcaster struct {
	rw     sync.RWMutex
	db     *geoIPDB
	dbName string

	waiter chan struct{}
}

// AvailableUpdate returns a new update artifact if the current broadcaster
// needs a database update.
func (ub *updateBroadcaster) AvailableUpdate() *updates.Artifact {
	ub.rw.RLock()
	defer ub.rw.RUnlock()

	// Get artifact.
	artifact, err := module.instance.IntelUpdates().GetFile(ub.dbName)
	if err != nil {
		// Check if the geoip database is included in the binary index instead.
		// TODO: Remove when intelhub builds the geoip database.
		if artifact2, err2 := module.instance.BinaryUpdates().GetFile(ub.dbName); err2 == nil {
			artifact = artifact2
			err = nil
		} else {
			log.Warningf("geoip: failed to get geoip update: %s", err)
			return nil
		}
	}

	// Return artifact if not yet initialized.
	if ub.db == nil {
		return artifact
	}

	// Compare and return artifact only when confirmed newer.
	if newer, _ := artifact.IsNewerThan(ub.db.update); newer {
		return artifact
	}
	return nil
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
		update := upd.v4.AvailableUpdate()
		if update != nil {
			if v4, err := getGeoIPDB(update); err == nil {
				upd.v4.ReplaceDatabase(v4)
			} else {
				log.Warningf("geoip: failed to get v4 database: %s", err)
			}
		}

		update = upd.v6.AvailableUpdate()
		if update != nil {
			if v6, err := getGeoIPDB(update); err == nil {
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

func getGeoIPDB(update *updates.Artifact) (*geoIPDB, error) {
	log.Debugf("geoip: opening database %s", update.Path())

	reader, err := maxminddb.Open(update.Path())
	if err != nil {
		return nil, fmt.Errorf("failed to open: %w", err)
	}
	log.Debugf("geoip: successfully opened database %s", update.Filename)

	return &geoIPDB{
		Reader: reader,
		update: update,
	}, nil
}
