package splittun

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
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
	return prepConfig()
}

func (s *SplitTunModule) Manager() *mgr.Manager {
	return s.mgr
}

func (s *SplitTunModule) Start() error {
	module.instance.Config().EventConfigChange.AddCallback("splittun enable check", func(wc *mgr.WorkerCtx, t struct{}) (bool, error) {
		if cfgOptionSplitTunEnable() {
			s.enable()
		} else {
			s.disable()
		}
		return false, nil
	})

	if cfgOptionSplitTunEnable() {
		s.enable()
	}

	return nil
}

func (s *SplitTunModule) Stop() error {
	return s.disable()
}

func (s *SplitTunModule) enable() error {
	if !ready.CompareAndSwap(false, true) {
		return nil // already enabled
	}
	s.mgr.Info("splittun: enabling Split Tunnel functionality")

	err := startProxies(s.mgr)
	if err != nil {
		s.mgr.Error("splittun: failed to start Split Tunnel proxies: ", err)
		ready.Store(false)
	}

	return err
}

func (s *SplitTunModule) disable() error {
	if !ready.CompareAndSwap(true, false) {
		return nil // already disabled
	}
	s.mgr.Info("splittun: disabling Split Tunnel functionality")

	clearPendingRequests()
	err := stopProxies()
	if err != nil {
		s.mgr.Error("splittun: failed to stop Split Tunnel proxies: ", err)
	}
	return err
}

// INSTANCE
type instance interface {
	Config() *config.Config
}
