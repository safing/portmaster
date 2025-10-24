package control

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/compat"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/mgr"
)

var logPrefix = "control: "

type Control struct {
	mgr      *mgr.Manager
	instance instance

	locker         sync.Mutex
	pauseWorker    *mgr.WorkerMgr
	isPaused       bool
	isPausedSPN    bool
	pauseStartTime time.Time
	pauseDuration  time.Duration
}

type instance interface {
	Config() *config.Config
	Interception() *interception.Interception
	Compat() *compat.Compat
	SPNGroup() *mgr.ExtendedGroup
}

var (
	singleton atomic.Bool
)

func New(instance instance) (*Control, error) {
	if !singleton.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("control: New failed: instance already created")
	}

	mgr := mgr.New("Control")
	module := &Control{
		mgr:      mgr,
		instance: instance,
	}
	if err := module.prep(); err != nil {
		return nil, err
	}
	return module, nil
}

func (c *Control) Manager() *mgr.Manager {
	return c.mgr
}

func (c *Control) Start() error {
	return nil
}

func (c *Control) Stop() error {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.stopResumeWorker()

	return nil
}

func (c *Control) prep() error {
	return c.registerAPIEndpoints()
}
