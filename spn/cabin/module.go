package cabin

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type Cabin struct {
	m        *mgr.Manager
	instance instance
}

func (c *Cabin) Manager() *mgr.Manager {
	return c.m
}

func (c *Cabin) Start() error {
	return nil
}

func (c *Cabin) Stop() error {
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

	m := mgr.New("Cabin")
	module = &Cabin{
		m:        m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
