package process

import (
	"os"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

var (
	module      *modules.Module
	updatesPath string
)

func init() {
	module = modules.Register("processes", prep, start, nil, "profiles")
}

func prep() error {
	return registerConfiguration()
}

func start() error {
	updatesPath = updates.RootPath() + string(os.PathSeparator)
	if updatesPath != "" {
		updatesPath += string(os.PathSeparator)
	}

	return nil
}
