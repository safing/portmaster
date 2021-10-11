package core

import (
	"flag"
	"fmt"
	"time"

	"github.com/safing/portbase/config"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	"github.com/tevino/abool"

	// module dependencies
	_ "github.com/safing/portmaster/netenv"
	_ "github.com/safing/portmaster/status"
	_ "github.com/safing/portmaster/ui"
	_ "github.com/safing/portmaster/updates"
)

const (
	eventShutdown = "shutdown"
	eventRestart  = "restart"
)

var (
	module *modules.Module

	restarting = abool.New()
	devMode    = config.Concurrent.GetAsBool(config.CfgDevModeKey, false)

	disableShutdownEvent bool
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

	flag.BoolVar(
		&disableShutdownEvent,
		"disable-shutdown-event",
		false,
		"disable shutdown event to keep app and notifier open when core shuts down",
	)

	modules.SetGlobalShutdownFn(shutdownHook)
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

func registerEvents() {
	module.RegisterEvent(eventShutdown, true)
	module.RegisterEvent(eventRestart, true)
}

func shutdownHook() {
	// Notify everyone of the restart/shutdown.
	if restarting.IsNotSet() {
		// Only trigger shutdown event if not disabled.
		if !disableShutdownEvent {
			module.TriggerEvent(eventShutdown, nil)
		}
	} else {
		module.TriggerEvent(eventRestart, nil)
	}

	// Wait a bit for the event to propagate.
	time.Sleep(100 * time.Millisecond)
}
