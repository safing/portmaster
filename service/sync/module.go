package sync

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/service/mgr"
)

type Sync struct {
	mgr      *mgr.Manager
	instance instance
}

func (s *Sync) Manager() *mgr.Manager {
	return s.mgr
}

func (s *Sync) Start() error {
	return nil
}

func (s *Sync) Stop() error {
	return nil
}

var db = database.NewInterface(&database.Options{
	Local:    true,
	Internal: true,
})

func prep() error {
	if err := registerSettingsAPI(); err != nil {
		return err
	}
	if err := registerSingleSettingAPI(); err != nil {
		return err
	}
	if err := registerProfileAPI(); err != nil {
		return err
	}
	return nil
}

var (
	module     *Sync
	shimLoaded atomic.Bool
)

// New returns a new NetEnv module.
func New(instance instance) (*Sync, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Sync")
	module = &Sync{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
