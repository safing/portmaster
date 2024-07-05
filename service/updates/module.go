package updates

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
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

	instance     instance
	shutdownFunc func(exitCode int)
}

var (
	module     *Updates
	shimLoaded atomic.Bool
)

// New returns a new UI module.
func New(instance instance, shutdownFunc func(exitCode int)) (*Updates, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("updates")
	module = &Updates{
		m:                     m,
		states:                m.NewStateMgr(),
		updateWorkerMgr:       m.NewWorkerMgr("updater", checkForUpdates, nil), //FIXME
		restartWorkerMgr:      m.NewWorkerMgr("updater", checkForUpdates, nil), //FIXME
		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),
		instance:              instance,
		shutdownFunc:          shutdownFunc,
	}

	return module, nil
}

// Manager returns the module manager.
func (u *Updates) Manager() *mgr.Manager {
	return u.m
}

// Start starts the module.
func (u *Updates) Start(m *mgr.Manager) error {
	if err := prep(); err != nil {
		return err
	}

	return start()
}

// Stop stops the module.
func (u *Updates) Stop(_ *mgr.Manager) error {
	return stop()
}

type instance interface {
	API() *api.API
	Config() *config.Config
}
