package network

import (
	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("network", nil, start, nil, "core", "processes")
}

func start() error {
	err := registerAsDatabase()
	if err != nil {
		return err
	}

	go cleaner()

	return nil
}
