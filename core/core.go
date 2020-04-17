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

	module = modules.Register("core", nil, start, nil, "base", "subsystems", "status", "updates", "api", "notifications", "ui")
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

	return nil
}
