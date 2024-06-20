package api

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
)

// API is the HTTP/Websockets API module.
type API struct {
	mgr      *mgr.Manager
	instance instance

	online bool
}

// Start starts the module.
func (api *API) Start(m *mgr.Manager) error {
	api.mgr = m
	if err := prep(); err != nil {
		return err
	}
	if err := start(); err != nil {
		return err
	}

	module.online = true
	return nil
}

// Stop stops the module.
func (api *API) Stop(_ *mgr.Manager) error {
	return start()
}

var (
	shimLoaded atomic.Bool
	module     *API
)

// New returns a new UI module.
func New(instance instance) (*API, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &API{
		instance: instance,
	}

	return module, nil
}

type instance interface {
	Config() *config.Config
}
