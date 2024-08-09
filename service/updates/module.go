package updates

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

// Updates provides access to released artifacts.
type Updates struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	updateWorkerMgr  *mgr.WorkerMgr
	restartWorkerMgr *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

	instance instance
}

var (
	module     *Updates
	shimLoaded atomic.Bool
)

// New returns a new UI module.
func New(instance instance) (*Updates, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("Updates")
	module = &Updates{
		m:      m,
		states: m.NewStateMgr(),

		updateWorkerMgr:       m.NewWorkerMgr("updater", checkForUpdates, nil),
		restartWorkerMgr:      m.NewWorkerMgr("automatic restart", automaticRestart, nil),
		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),
		instance:              instance,
	}
	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
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
	return start()
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
