package base

import (
	"errors"
	"flag"

	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
)

// Default Values (changeable for testing)
var (
	DefaultAPIListenAddress = "127.0.0.1:817"

	dataDir     string
	databaseDir string
)

func init() {
	flag.StringVar(&dataDir, "data", "", "set data directory")
	flag.StringVar(&databaseDir, "db", "", "alias to --data (deprecated)")

	modules.SetGlobalPrepFn(globalPrep)
}

func globalPrep() error {
	if dataroot.Root() == nil {
		// initialize data dir

		// backwards compatibility
		if dataDir == "" {
			dataDir = databaseDir
		}

		// check data dir
		if dataDir == "" {
			return errors.New("please set the data directory using --data=/path/to/data/dir")
		}

		// initialize structure
		err := dataroot.Initialize(dataDir, 0755)
		if err != nil {
			return err
		}
	}

	// set api listen address
	api.SetDefaultAPIListenAddress(DefaultAPIListenAddress)

	// set notification persistence
	notifications.SetPersistenceBasePath("core:notifications")

	// set subsystem status dir
	subsystems.SetDatabaseKeySpace("core:status/subsystems")

	return nil
}
