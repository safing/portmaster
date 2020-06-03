package base

import (
	"github.com/safing/portbase/modules"

	// module dependencies
	_ "github.com/safing/portbase/config"
	_ "github.com/safing/portbase/rng"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("base", nil, start, nil, "database", "config", "rng")

	// For prettier subsystem graph, printed with --print-subsystem-graph
	/*
		subsystems.Register(
			"base",
			"Base",
			"THE GROUND.",
			baseModule,
			"",
			nil,
		)
	*/
}

func start() error {
	startProfiling()

	return registerDatabases()
}
