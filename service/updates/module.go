package updates

import (
	"errors"
	"flag"
	"fmt"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates/registry"
)

var applyUpdates bool

func init() {
	flag.BoolVar(&applyUpdates, "update", false, "apply downloaded updates")
}

// Updates provides access to released artifacts.
type Updates struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	updateBinaryWorkerMgr *mgr.WorkerMgr
	updateIntelWorkerMgr  *mgr.WorkerMgr
	restartWorkerMgr      *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

	registry registry.Registry

	instance instance
}

var shimLoaded atomic.Bool

// New returns a new UI module.
func New(instance instance) (*Updates, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("Updates")
	module := &Updates{
		m:      m,
		states: m.NewStateMgr(),

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),

		instance: instance,
	}

	// Events
	module.updateBinaryWorkerMgr = m.NewWorkerMgr("binary updater", module.checkForBinaryUpdates, nil)
	module.updateIntelWorkerMgr = m.NewWorkerMgr("intel updater", module.checkForIntelUpdates, nil)
	module.restartWorkerMgr = m.NewWorkerMgr("automatic restart", automaticRestart, nil)

	binIndex := registry.UpdateIndex{
		Directory:         "/usr/lib/portmaster",
		DownloadDirectory: "/var/lib/portmaster/new_bin",
		Ignore:            []string{"databases", "intel", "config.json"},
		IndexURLs:         []string{"http://localhost:8000/test-binary.json"},
		IndexFile:         "bin-index.json",
		AutoApply:         false,
	}

	intelIndex := registry.UpdateIndex{
		Directory:         "/var/lib/portmaster/intel",
		DownloadDirectory: "/var/lib/portmaster/new_intel",
		IndexURLs:         []string{"http://localhost:8000/test-intel.json"},
		IndexFile:         "intel-index.json",
		AutoApply:         true,
	}
	module.registry = registry.New(binIndex, intelIndex)

	return module, nil
}

func (u *Updates) checkForBinaryUpdates(_ *mgr.WorkerCtx) error {
	hasUpdates, err := u.registry.CheckForBinaryUpdates()
	if err != nil {
		log.Errorf("updates: failed to check for binary updates: %s", err)
	}
	if hasUpdates {
		log.Infof("updates: there is updates available in the binary bundle")
		err = u.registry.DownloadBinaryUpdates()
		if err != nil {
			log.Errorf("updates: failed to download bundle: %s", err)
		}
	} else {
		log.Infof("updates: no new binary updates")
	}
	return nil
}

func (u *Updates) checkForIntelUpdates(_ *mgr.WorkerCtx) error {
	hasUpdates, err := u.registry.CheckForIntelUpdates()
	if err != nil {
		log.Errorf("updates: failed to check for intel updates: %s", err)
	}
	if hasUpdates {
		log.Infof("updates: there is updates available in the intel bundle")
		err = u.registry.DownloadIntelUpdates()
		if err != nil {
			log.Errorf("updates: failed to download bundle: %s", err)
		}
	} else {
		log.Infof("updates: no new intel data updates")
	}
	return nil
}

// States returns the state manager.
func (u *Updates) States() *mgr.StateMgr {
	return u.states
}

// Manager returns the module manager.
func (u *Updates) Manager() *mgr.Manager {
	return u.m
}

// Start starts the module.
func (u *Updates) Start() error {
	// initConfig()

	if applyUpdates {
		err := u.registry.ApplyBinaryUpdates()
		if err != nil {
			log.Errorf("updates: failed to apply binary updates: %s", err)
		}
		err = u.registry.ApplyIntelUpdates()
		if err != nil {
			log.Errorf("updates: failed to apply intel updates: %s", err)
		}
		u.instance.Restart()
		return nil
	}

	err := u.registry.Initialize()
	if err != nil {
		// TODO(vladimir): Find a better way to handle this error. The service will stop if parsing of the bundle files fails.
		return fmt.Errorf("failed to initialize registry: %w", err)
	}

	u.updateBinaryWorkerMgr.Go()
	u.updateIntelWorkerMgr.Go()
	return nil
}

func (u *Updates) GetFile(id string) (*registry.File, error) {
	return u.registry.GetFile(id)
}

// Stop stops the module.
func (u *Updates) Stop() error {
	return stop()
}

type instance interface {
	API() *api.API
	Config() *config.Config
	Restart()
	Shutdown()
	Notifications() *notifications.Notifications
}
