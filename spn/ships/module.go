package ships

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type Ships struct {
	mgr      *mgr.Manager
	instance instance
}

func (s *Ships) Start(m *mgr.Manager) error {
	s.mgr = m
	if conf.PublicHub() {
		initPageInput()
	}

	return nil
}

func (s *Ships) Stop(m *mgr.Manager) error {
	return nil
}

var (
	module     *Ships
	shimLoaded atomic.Bool
)

// New returns a new Config module.
func New(instance instance) (*Ships, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Ships{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
