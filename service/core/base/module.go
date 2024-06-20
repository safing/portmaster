package base

import (
	"errors"
	"sync/atomic"

	_ "github.com/safing/portmaster/base/config"
	_ "github.com/safing/portmaster/base/metrics"
	_ "github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
)

type Base struct {
	mgr      *mgr.Manager
	instance instance
}

func (b *Base) Start(m *mgr.Manager) error {
	b.mgr = m
	startProfiling()

	if err := registerDatabases(); err != nil {
		return err
	}

	registerLogCleaner()

	return nil
}

func (b *Base) Stop(m *mgr.Manager) error {
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

	module = &Base{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
