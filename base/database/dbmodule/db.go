package dbmodule

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
)

type DBModule struct {
	mgr      *mgr.Manager
	instance instance
}

func (dbm *DBModule) Manager() *mgr.Manager {
	return dbm.mgr
}

func (dbm *DBModule) Start() error {
	return start()
}

func (dbm *DBModule) Stop() error {
	return stop()
}

var databaseStructureRoot *utils.DirStructure

// SetDatabaseLocation sets the location of the database for initialization. Supply either a path or dir structure.
func SetDatabaseLocation(dirStructureRoot *utils.DirStructure) {
	if databaseStructureRoot == nil {
		databaseStructureRoot = dirStructureRoot
	}
}

func prep() error {
	SetDatabaseLocation(dataroot.Root())
	if databaseStructureRoot == nil {
		return errors.New("database location not specified")
	}

	return nil
}

func start() error {
	startMaintenanceTasks()
	return nil
}

func stop() error {
	return database.Shutdown()
}

var (
	module     *DBModule
	shimLoaded atomic.Bool
)

func New(instance instance) (*DBModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	if err := prep(); err != nil {
		return nil, err
	}
	m := mgr.New("DBModule")
	module = &DBModule{
		mgr:      m,
		instance: instance,
	}

	err := database.Initialize(databaseStructureRoot)
	if err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
