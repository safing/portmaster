package splittun

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
)

const SplitTunPort = 719

type SplitTunModule struct {
	mgr      *mgr.Manager
	instance instance
}

var (
	module     *SplitTunModule
	shimLoaded atomic.Bool
	ready      atomic.Bool // ready indicates whether the module is fully initialized and ready to handle requests.
)

func IsReady() bool {
	return ready.Load()
}

func New(instance instance) (*SplitTunModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("SplitTunModule")
	module = &SplitTunModule{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

func prep() error {
	return nil
}

func (s *SplitTunModule) Manager() *mgr.Manager {
	return s.mgr
}

func (s *SplitTunModule) Start() error {
	err := startProxies(s.mgr)
	if err != nil {
		return err
	}
	ready.Store(true)
	return nil
}

func (s *SplitTunModule) Stop() error {
	ready.Store(false)
	return stopProxies()
}

// INSTANCE
type instance interface{}
