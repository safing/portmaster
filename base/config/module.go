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

// Manager returns the module's manager.
func (u *Config) Manager() *mgr.Manager {
	return u.mgr
}

// Start starts the module.
func (u *Config) Start() error {
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

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	SetCmdLineOperation(f func() error)
}
