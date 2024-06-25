package config

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
)

// Config provides configuration mgmt.
type Config struct {
	mgr *mgr.Manager

	instance instance

	EventConfigChange *mgr.EventMgr[struct{}]
}

// Start starts the module.
func (u *Config) Start(m *mgr.Manager) error {
	u.mgr = m
	u.EventConfigChange = mgr.NewEventMgr[struct{}](ChangeEvent, u.mgr)

	if err := prep(); err != nil {
		return err
	}
	return start()
}

// Stop stops the module.
func (u *Config) Stop(_ *mgr.Manager) error {
	return nil
}

var (
	module     *Config
	shimLoaded atomic.Bool
)

// New returns a new Config module.
func New(instance instance) (*Config, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Config{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
