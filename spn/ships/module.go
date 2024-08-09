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

func (s *Ships) Manager() *mgr.Manager {
	return s.mgr
}

func (s *Ships) Start() error {
	if conf.PublicHub() {
		initPageInput()
	}

	return nil
}

func (s *Ships) Stop() error {
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
	m := mgr.New("Ships")
	module = &Ships{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface{}
