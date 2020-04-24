package core

import (
	"fmt"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"

	// module dependencies
	_ "github.com/safing/portbase/rng"
	_ "github.com/safing/portmaster/status"
	_ "github.com/safing/portmaster/ui"
	_ "github.com/safing/portmaster/updates"
)

var (
	module *modules.Module
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

	module = modules.Register("core", prep, start, nil, "base", "subsystems", "status", "updates", "api", "notifications", "ui", "netenv", "network", "interception")
	subsystems.Register(
		"core",
		"Core",
		"Base Structure and System Integration",
		module,
		"config:core/",
		nil,
	)
}

func prep() error {
	registerEvents()
	return nil
}

func start() error {
	if err := startPlatformSpecific(); err != nil {
		return fmt.Errorf("failed to start plattform-specific components: %s", err)
	}

	if err := registerEventHooks(); err != nil {
		return err
	}

	return nil
}
