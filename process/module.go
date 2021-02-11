package process

import (
	"os"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

var (
	module      *modules.Module
	updatesPath string
)

func init() {
	module = modules.Register("processes", prep, start, nil, "profiles", "updates")
}

func prep() error {
	return registerConfiguration()
}

func start() error {
	updatesPath = updates.RootPath()
	if updatesPath != "" {
		updatesPath += string(os.PathSeparator)
	}
	log.Warningf("process: using updates path %s", updatesPath)

	return nil
}
