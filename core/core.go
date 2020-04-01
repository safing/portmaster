package core

import (
	"fmt"

	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("core", nil, start, nil, "database", "config", "api", "random", "notifications", "subsystems", "ui", "updates", "status")
	subsystems.Register(
		"core",
		"Core",
		"Base Structure and System Integration",
		module,
		"config:core/",
		nil,
	)
}

func start() error {
	if err := startPlatformSpecific(); err != nil {
		return fmt.Errorf("failed to start plattform-specific components: %s", err)
	}

	return registerDatabases()
}
