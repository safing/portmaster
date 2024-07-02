package cabin

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type Cabin struct {
	instance instance
}

func (c *Cabin) Start(m *mgr.Manager) error {
	return prep()
}

func (c *Cabin) Stop(m *mgr.Manager) error {
	return nil
}

var (
	module     *Cabin
	shimLoaded atomic.Bool
)

func prep() error {
	if err := initProvidedExchKeySchemes(); err != nil {
		return err
	}

	if conf.PublicHub() {
		if err := prepPublicHubConfig(); err != nil {
			return err
		}
	}

	return nil
}

// New returns a new Cabin module.
func New(instance instance) (*Cabin, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	if err := prep(); err != nil {
		return nil, err
	}

	module = &Cabin{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
