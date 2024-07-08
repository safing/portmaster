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

func (u *Config) Manager() *mgr.Manager {
	return u.mgr
}

// Start starts the module.
func (u *Config) Start() error {
	if err := prep(); err != nil {
		return err
	}
	return start()
}

// Stop stops the module.
func (u *Config) Stop() error {
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
	m := mgr.New("Config")
	module = &Config{
		mgr:               m,
		instance:          instance,
		EventConfigChange: mgr.NewEventMgr[struct{}](ChangeEvent, m),
	}
	return module, nil
}

type instance interface{}
