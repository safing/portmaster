package process

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

type ProcessModule struct {
	mgr      *mgr.Manager
	instance instance

	portmasterUIPath string
}

func (pm *ProcessModule) Manager() *mgr.Manager {
	return pm.mgr
}

func (pm *ProcessModule) Start() error {
	file, err := pm.instance.BinaryUpdates().GetFile("portmaster")
	if err != nil {
		log.Errorf("process: failed to get path of ui: %s", err)
	} else {
		pm.portmasterUIPath = file.Path()
	}
	return nil
}

func (pm *ProcessModule) Stop() error {
	return nil
}

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

type instance interface {
	BinaryUpdates() *updates.Updates
}
