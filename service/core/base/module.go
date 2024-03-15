package base

import (
	_ "github.com/safing/portbase/config"
	_ "github.com/safing/portbase/metrics"
	"github.com/safing/portbase/modules"
	_ "github.com/safing/portbase/rng"
)

var module *modules.Module

func init() {
	module = modules.Register("base", nil, start, nil, "database", "config", "rng", "metrics")

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

	if err := registerDatabases(); err != nil {
		return err
	}

	registerLogCleaner()

	return nil
}
