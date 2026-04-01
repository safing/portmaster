package control

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

type PauseInfo struct {
	Interception bool      // Whether Portmaster interception is paused
	SPN          bool      // Whether SPN is paused
	TillTime     time.Time // When the pause will end
}

type Control struct {
	mgr      *mgr.Manager
	instance instance
	states   *mgr.StateMgr

	locker            sync.Mutex
	resumeWorker      *mgr.WorkerMgr
	pauseNotification *notifications.Notification
	pauseInfo         PauseInfo
}

type instance interface {
	Config() *config.Config
	InterceptionGroup() *mgr.GroupModule
	SPNGroup() *mgr.ExtendedGroup
	IsShuttingDown() bool
}

var (
	singleton atomic.Bool
)

func New(instance instance) (*Control, error) {
	if !singleton.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("control: New failed: instance already created")
	}

	m := mgr.New("Control")
	module := &Control{
		mgr:      m,
		instance: instance,
		states:   mgr.NewStateMgr(m),
	}
	if err := module.prep(); err != nil {
		return nil, err
	}
	return module, nil
}

func (c *Control) Manager() *mgr.Manager {
	return c.mgr
}

func (u *Control) States() *mgr.StateMgr {
	return u.states
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
