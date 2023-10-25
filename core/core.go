package core

import (
	"flag"
	"fmt"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/metrics"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	_ "github.com/safing/portmaster/broadcasts"
	_ "github.com/safing/portmaster/netenv"
	_ "github.com/safing/portmaster/netquery"
	_ "github.com/safing/portmaster/status"
	_ "github.com/safing/portmaster/sync"
	_ "github.com/safing/portmaster/ui"
	"github.com/safing/portmaster/updates"
)

const (
	eventShutdown = "shutdown"
	eventRestart  = "restart"
)

var (
	module *modules.Module

	disableShutdownEvent bool
)

func init() {
	module = modules.Register("core", prep, start, nil, "base", "subsystems", "status", "updates", "api", "notifications", "ui", "netenv", "network", "netquery", "interception", "compat", "broadcasts", "sync")
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
		return fmt.Errorf("failed to start plattform-specific components: %w", err)
	}

	// Enable persistent metrics.
	if err := metrics.EnableMetricPersistence("core:metrics/storage"); err != nil {
		log.Warningf("core: failed to enable persisted metrics: %s", err)
	}

	return nil
}

func registerEvents() {
	module.RegisterEvent(eventShutdown, true)
	module.RegisterEvent(eventRestart, true)
}

func shutdownHook() {
	// Notify everyone of the restart/shutdown.
	if !updates.IsRestarting() {
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
