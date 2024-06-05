package module

import (
	"flag"
	"fmt"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/modules"
)

var showVersion bool

func init() {
	modules.Register("info", prep, nil, nil)

	flag.BoolVar(&showVersion, "version", false, "show version and exit")
}

func prep() error {
	err := info.CheckVersion()
	if err != nil {
		return err
	}

	if printVersion() {
		return modules.ErrCleanExit
	}
	return nil
}

// printVersion prints the version, if requested, and returns if it did so.
func printVersion() (printed bool) {
	if showVersion {
		fmt.Println(info.FullVersion())
		return true
	}
	return false
}
