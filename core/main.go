package core

import (
	"errors"
	"flag"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"

	"github.com/safing/portmaster/core/structure"
)

var (
	dataDir     string
	databaseDir string

	shuttingDown = make(chan struct{})
)

func init() {
	flag.StringVar(&dataDir, "data", "", "set data directory")
	flag.StringVar(&databaseDir, "db", "", "alias to --data (deprecated)")

	modules.Register("core", prep, start, stop)

	notifications.SetPersistenceBasePath("core:notifications")
}

func prep() error {
	// backwards compatibility
	if dataDir == "" {
		dataDir = databaseDir
	}

	// check data dir
	if dataDir == "" {
		return errors.New("please set the data directory using --data=/path/to/data/dir")
	}

	// initialize structure
	return structure.Initialize(dataDir, 0755)
}

func start() error {
	// init DB
	err := startDB()
	if err != nil {
		return err
	}

	// register DBs
	return registerDatabases()
}

func stop() error {
	close(shuttingDown)
	return stopDB()
}
