package network

import (
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/network/environment"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("network", nil, start, nil, "core")
	environment.InitSubModule(module)
}

func start() error {
	err := registerAsDatabase()
	if err != nil {
		return err
	}

	go cleaner()

	return environment.StartSubModule()
}
