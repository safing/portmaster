package core

import "github.com/safing/portbase/modules"

var (
	coreModule = modules.Register("core", nil, startCore, nil, "base", "database", "config", "api", "random")
)

func startCore() error {
	return registerDatabases()
}
