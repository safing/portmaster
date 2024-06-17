package sync

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/service/mgr"
)

type Sync struct {
	instance instance
}

func (s *Sync) Start(m *mgr.Manager) error {
	return prep()
}

func (s *Sync) Stop(m *mgr.Manager) error {
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

	module = &Sync{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
