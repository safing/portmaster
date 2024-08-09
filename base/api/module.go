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

	online atomic.Bool
}

func (api *API) Manager() *mgr.Manager {
	return api.mgr
}

// Start starts the module.
func (api *API) Start() error {
	if err := start(); err != nil {
		return err
	}

	api.online.Store(true)
	return nil
}

// Stop stops the module.
func (api *API) Stop() error {
	defer api.online.Store(false)
	return stop()
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
	m := mgr.New("API")
	module = &API{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}
	return module, nil
}

type instance interface {
	Config() *config.Config
	SetCmdLineOperation(f func() error)
	Ready() bool
}
