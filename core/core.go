package core

import (
	"fmt"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"

	// module dependencies
	_ "github.com/safing/portmaster/netenv"
	_ "github.com/safing/portmaster/status"
	_ "github.com/safing/portmaster/ui"
	_ "github.com/safing/portmaster/updates"
)

var (
	module *modules.Module
)

func init() {
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

	// init config
	err := registerConfig()
	if err != nil {
		return err
	}

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

func start() error {
	if err := startPlatformSpecific(); err != nil {
		return fmt.Errorf("failed to start plattform-specific components: %s", err)
	}

	registerLogCleaner()

	return nil
}
