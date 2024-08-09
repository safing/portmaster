package base

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
)

// Base is the base module.
type Base struct {
	mgr      *mgr.Manager
	instance instance
}

// Manager returns the module manager.
func (b *Base) Manager() *mgr.Manager {
	return b.mgr
}

// Start starts the module.
func (b *Base) Start() error {
	startProfiling()
	registerLogCleaner()

	return nil
}

// Stop stops the module.
func (b *Base) Stop() error {
	return nil
}

var (
	module     *Base
	shimLoaded atomic.Bool
)

// New returns a new Base module.
func New(instance instance) (*Base, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Base")
	module = &Base{
		mgr:      m,
		instance: instance,
	}

	if err := prep(instance); err != nil {
		return nil, err
	}
	if err := registerDatabases(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	SetCmdLineOperation(f func() error)
}
