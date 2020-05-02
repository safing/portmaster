package base

import (
	"github.com/safing/portbase/modules"

	// module dependencies
	_ "github.com/safing/portbase/config"
	_ "github.com/safing/portbase/rng"
)

func init() {
	modules.Register("base", nil, registerDatabases, nil, "database", "config", "rng")

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
