package core

import (
	"errors"
	"flag"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	_ "github.com/safing/portmaster/service/broadcasts"
	"github.com/safing/portmaster/service/mgr"
	_ "github.com/safing/portmaster/service/netenv"
	_ "github.com/safing/portmaster/service/netquery"
	_ "github.com/safing/portmaster/service/status"
	_ "github.com/safing/portmaster/service/sync"
	_ "github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
)

const (
	eventShutdown = "shutdown"
	eventRestart  = "restart"
)

type Core struct {
	m        *mgr.Manager
	instance instance

	EventShutdown *mgr.EventMgr[struct{}]
	EventRestart  *mgr.EventMgr[struct{}]
}

func (c *Core) Manager() *mgr.Manager {
	return c.m
}

func (c *Core) Start() error {
	return start()
}

func (c *Core) Stop() error {
	return nil
}

var disableShutdownEvent bool

func init() {
	// module = modules.Register("core", prep, start, nil, "base", "subsystems", "status", "updates", "api", "notifications", "ui", "netenv", "network", "netquery", "interception", "compat", "broadcasts", "sync")
	// subsystems.Register(
	// 	"core",
	// 	"Core",
	// 	"Base Structure and System Integration",
	// 	module,
	// 	"config:core/",
	// 	nil,
	// )

	flag.BoolVar(
		&disableShutdownEvent,
		"disable-shutdown-event",
		false,
		"disable shutdown event to keep app and notifier open when core shuts down",
	)

	// modules.SetGlobalShutdownFn(shutdownHook)
}

func prep() error {
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

func ShutdownHook() {
	// Notify everyone of the restart/shutdown.
	if !updates.IsRestarting() {
		// Only trigger shutdown event if not disabled.
		if !disableShutdownEvent {
			module.EventShutdown.Submit(struct{}{})
		}
	} else {
		module.EventRestart.Submit(struct{}{})
	}

	// Wait a bit for the event to propagate.
	// TODO(vladimir): is this necessary?
	time.Sleep(100 * time.Millisecond)
}

var (
	module     *Core
	shimLoaded atomic.Bool
)

// New returns a new NetEnv module.
func New(instance instance) (*Core, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("Core")
	module = &Core{
		m:        m,
		instance: instance,

		EventShutdown: mgr.NewEventMgr[struct{}]("shutdown", m),
		EventRestart:  mgr.NewEventMgr[struct{}]("restart", m),
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
