package updates

import (
	"flag"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates/registry"
)

var autoUpdate bool

func init() {
	flag.BoolVar(&autoUpdate, "auto-update", false, "auto apply downloaded updates")
}

// Updates provides access to released artifacts.
type Updates struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	updateCheckWorkerMgr *mgr.WorkerMgr
	upgraderWorkerMgr    *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

	registry registry.Registry

	instance instance
}

// New returns a new Updates module.
func New(instance instance, name string, index registry.UpdateIndex) (*Updates, error) {
	m := mgr.New(name)
	module := &Updates{
		m:      m,
		states: m.NewStateMgr(),

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),

		instance: instance,
	}

	// Events
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.checkForUpdates, nil)
	module.updateCheckWorkerMgr.Repeat(30 * time.Second)
	module.upgraderWorkerMgr = m.NewWorkerMgr("upgrader", func(w *mgr.WorkerCtx) error {
		err := module.registry.ApplyUpdates()
		if err != nil {
			// TODO(vladimir): Send notification to UI
			log.Errorf("updates: failed to apply updates: %s", err)
		} else {
			module.instance.Restart()
		}
		return nil
	}, nil)

	module.registry = registry.New(index)
	_ = module.registry.Initialize()

	return module, nil
}

func (u *Updates) checkForUpdates(_ *mgr.WorkerCtx) error {
	hasUpdates, err := u.registry.CheckForUpdates()
	if err != nil {
		log.Errorf("updates: failed to check for updates: %s", err)
	}
	if hasUpdates {
		log.Infof("updates: there is updates available")
		err = u.registry.DownloadUpdates()
		if err != nil {
			log.Errorf("updates: failed to download bundle: %s", err)
		} else if autoUpdate {
			u.ApplyUpdates()
		}
	} else {
		log.Infof("updates: no new updates")
		u.EventResourcesUpdated.Submit(struct{}{})
	}
	return nil
}

func (u *Updates) ApplyUpdates() {
	u.upgraderWorkerMgr.Go()
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
	u.updateCheckWorkerMgr.Go()
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
