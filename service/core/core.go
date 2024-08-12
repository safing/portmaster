package core

import (
	"errors"
	"flag"
	"fmt"
	"sync/atomic"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	_ "github.com/safing/portmaster/service/broadcasts"
	"github.com/safing/portmaster/service/mgr"
	_ "github.com/safing/portmaster/service/netenv"
	_ "github.com/safing/portmaster/service/netquery"
	_ "github.com/safing/portmaster/service/status"
	_ "github.com/safing/portmaster/service/sync"
	_ "github.com/safing/portmaster/service/ui"
)

// Core is the core service module.
type Core struct {
	m        *mgr.Manager
	instance instance

<<<<<<< HEAD
type Core struct {
	m        *mgr.Manager
	instance instance
||||||| 151a548c
var (
	module *modules.Module
=======
	EventShutdown *mgr.EventMgr[struct{}]
	EventRestart  *mgr.EventMgr[struct{}]
}
>>>>>>> develop

<<<<<<< HEAD
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
||||||| 151a548c
	disableShutdownEvent bool
)
=======
// Manager returns the manager.
func (c *Core) Manager() *mgr.Manager {
	return c.m
}

// Start starts the module.
func (c *Core) Start() error {
	return start()
}

// Stop stops the module.
func (c *Core) Stop() error {
	return nil
}

var disableShutdownEvent bool
>>>>>>> develop

func init() {
	flag.BoolVar(
		&disableShutdownEvent,
		"disable-shutdown-event",
		false,
		"disable shutdown event to keep app and notifier open when core shuts down",
	)
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

	if err := initModulesIntegration(); err != nil {
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

type instance interface {
	Shutdown()
}
