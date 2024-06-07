package module

import (
	"errors"
	"flag"
	"fmt"
	"sync/atomic"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/service/mgr"
)

type Info struct {
	instance instance
}

var showVersion bool

func init() {
	flag.BoolVar(&showVersion, "version", false, "show version and exit")
}

func (i *Info) Start(m *mgr.Manager) error {
	err := info.CheckVersion()
	if err != nil {
		return err
	}

	if printVersion() {
		return modules.ErrCleanExit
	}
	return nil
}

func (i *Info) Stop(m *mgr.Manager) error {
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

var shimLoaded atomic.Bool

func New(instance instance) (*Info, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	module := &Info{
		instance: instance,
	}

	return module, nil
}

type instance interface{}
