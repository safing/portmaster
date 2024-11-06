package base

import (
	"errors"
	"flag"
	"fmt"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/service/mgr"
)

// Default Values (changeable for testing).
var (
	DefaultAPIListenAddress = "127.0.0.1:817"

	showVersion bool
)

func init() {
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

	// set api listen address
	api.SetDefaultAPIListenAddress(DefaultAPIListenAddress)

	return nil
}

func printVersion() error {
	fmt.Println(info.FullVersion())
	return nil
}
