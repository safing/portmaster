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
	mgr *mgr.Manager

	instance     instance
	shutdownFunc func(exitCode int)

	updateWorkerMgr  *mgr.WorkerMgr
	restartWorkerMgr *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

	States *mgr.StateMgr
}

// Start starts the module.
func (u *Updates) Start(m *mgr.Manager) error {
	u.mgr = m
	u.EventResourcesUpdated = mgr.NewEventMgr[struct{}](ResourceUpdateEvent, u.mgr)
	u.EventVersionsUpdated = mgr.NewEventMgr[struct{}](VersionUpdateEvent, u.mgr)
	u.States = mgr.NewStateMgr(u.mgr)

	if err := prep(); err != nil {
		return err
	}

	return start()
}

// Stop stops the module.
func (u *Updates) Stop(_ *mgr.Manager) error {
	return stop()
}

var (
	module     *Updates
	shimLoaded atomic.Bool
)

// New returns a new UI module.
func New(instance instance, shutdownFunc func(exitCode int)) (*Updates, error) {
	if shimLoaded.CompareAndSwap(false, true) {
		module = &Updates{
			instance:     instance,
			shutdownFunc: shutdownFunc,
		}
		return module, nil
	}
	return nil, errors.New("only one instance allowed")
}

type instance interface {
	API() *api.API
	Config() *config.Config
}
