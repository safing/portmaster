package process

import (
	"os"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/service/updates"
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

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}
