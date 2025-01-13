package base

import (
	"errors"
	"flag"
	"fmt"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
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
}

func prep(instance instance) error {
	// check if meta info is ok
	err := info.CheckVersion()
	if err != nil {
		return errors.New("compile error: please compile using the provided build script")
	}

	// print version
	if showVersion {
		instance.SetCmdLineOperation(printVersion)
		return mgr.ErrExecuteCmdLineOp
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
		err := dataroot.Initialize(dataDir, utils.PublicReadPermission)
		if err != nil {
			return err
		}
	}

	// set api listen address
	api.SetDefaultAPIListenAddress(DefaultAPIListenAddress)

	return nil
}

func printVersion() error {
	fmt.Println(info.FullVersion())
	return nil
}
