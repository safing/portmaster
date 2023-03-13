package base

import (
	_ "github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/metrics"
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

	// Set metrics storage key and load them from db.
	if err := metrics.EnableMetricPersistence("core:metrics/storage"); err != nil {
		log.Warningf("core: failed to load persisted metrics from db: %s", err)
	}

	registerLogCleaner()

	return nil
}
