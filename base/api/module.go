package api

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
)

// API is the HTTP/Websockets API module.
type API struct {
	instance instance
}

// Start starts the module.
func (api *API) Start(_ *mgr.Manager) error {
	return start()
}

// Stop stops the module.
func (api *API) Stop(_ *mgr.Manager) error {
	return start()
}

var (
	shimLoaded atomic.Bool
)

// New returns a new UI module.
func New(instance instance) (*API, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return &API{
		instance: instance,
	}, nil
}

type instance interface {
}
