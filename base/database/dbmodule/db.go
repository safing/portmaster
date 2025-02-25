package dbmodule

import (
	"errors"
	"path/filepath"
	"sync/atomic"

	"github.com/safing/portmaster/base/database"
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

var databasesRootDir string

// SetDatabaseLocation sets the location of the database for initialization. Supply either a path or dir structure.
func SetDatabaseLocation(dir string) {
	if databasesRootDir == "" {
		databasesRootDir = dir
	}
}

// GetDatabaseLocation returns the initialized database location.
func GetDatabaseLocation() string {
	return databasesRootDir
}

func prep() error {
	SetDatabaseLocation(filepath.Join(module.instance.DataDir(), "databases"))
	if databasesRootDir == "" {
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

	m := mgr.New("DBModule")
	module = &DBModule{
		mgr:      m,
		instance: instance,
	}
	if err := prep(); err != nil {
		return nil, err
	}

	err := database.Initialize(databasesRootDir)
	if err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	DataDir() string
}
