package process

import (
	"errors"
	"os"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

type ProcessModule struct {
	mgr      *mgr.Manager
	instance instance
}

func (pm *ProcessModule) Manager() *mgr.Manager {
	return pm.mgr
}

func (pm *ProcessModule) Start() error {
	updatesPath = updates.RootPath()
	if updatesPath != "" {
		updatesPath += string(os.PathSeparator)
	}
	return nil
}

func (pm *ProcessModule) Stop() error {
	return nil
}

var updatesPath string

func prep() error {
	if err := registerConfiguration(); err != nil {
		return err
	}

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

var (
	module     *ProcessModule
	shimLoaded atomic.Bool
)

// New returns a new Process module.
func New(instance instance) (*ProcessModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("ProcessModule")
	module = &ProcessModule{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}
	return module, nil
}

type instance interface{}
