package ui

import (
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
)

var module *modules.Module

func init() {
	module = modules.Register("ui", prep, start, nil, "api", "updates")
}

func prep() error {
	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return registerRoutes()
}

func start() error {
	// Create a dummy directory to which processes change their working directory
	// to. Currently this includes the App and the Notifier. The aim is protect
	// all other directories and increase compatibility should any process want
	// to read or write something to the current working directory. This can also
	// be useful in the future to dump data to for debugging. The permission used
	// may seem dangerous, but proper permission on the parent directory provide
	// (some) protection.
	// Processes must _never_ read from this directory.
	err := dataroot.Root().ChildDir("exec", 0o0777).Ensure()
	if err != nil {
		log.Warningf("ui: failed to create safe exec dir: %s", err)
	}

	return nil
}
