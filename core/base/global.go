package base

import (
	"errors"
	"flag"
	"fmt"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/modules"
)

// Default Values (changeable for testing).
var (
	DefaultAPIListenAddress = "127.0.0.1:817"

	dataDir     string
	databaseDir string
	showVersion bool
)

func init() {
	flag.StringVar(&dataDir, "data", "", "set data directory")
	flag.StringVar(&databaseDir, "db", "", "alias to --data (deprecated)")
	flag.BoolVar(&showVersion, "version", false, "show version and exit")

	modules.SetGlobalPrepFn(globalPrep)
}

func globalPrep() error {
	// check if meta info is ok
	err := info.CheckVersion()
	if err != nil {
		return errors.New("compile error: please compile using the provided build script")
	}

	// print version
	if showVersion {
		fmt.Println(info.FullVersion())
		return modules.ErrCleanExit
	}

	// check data root
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
		err := dataroot.Initialize(dataDir, 0o0755)
		if err != nil {
			return err
		}
	}

	// set api listen address
	api.SetDefaultAPIListenAddress(DefaultAPIListenAddress)

	return nil
}
