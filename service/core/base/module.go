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

func (b *Base) Manager() *mgr.Manager {
	return b.mgr
}

func (b *Base) Start() error {
	startProfiling()
	registerLogCleaner()

	return nil
}

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

	if err := registerDatabases(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
