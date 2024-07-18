package status

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
)

// Status Module manages status information.
type Status struct {
	mgr      *mgr.Manager
	instance instance

	publishUpdate runtime.PushFunc
	triggerUpdate chan struct{}

	states     map[string]mgr.StateUpdate
	statesLock sync.Mutex

	notifications     map[string]map[string]*notifications.Notification
	notificationsLock sync.Mutex
}

// Manager returns the module manager.
func (s *Status) Manager() *mgr.Manager {
	return s.mgr
}

// Start starts the module.
func (s *Status) Start() error {
	if err := s.setupRuntimeProvider(); err != nil {
		return err
	}

	s.mgr.Go("status publisher", s.statusPublisher)

	s.instance.NetEnv().EventOnlineStatusChange.AddCallback("update online status in system status",
		func(_ *mgr.WorkerCtx, _ netenv.OnlineStatus) (bool, error) {
			s.triggerPublishStatus()
			return false, nil
		},
	)

	// Make an initial status query.
	s.statesLock.Lock()
	defer s.statesLock.Unlock()
	// Add status callback within the lock so we can force the right order.
	s.instance.AddStatesCallback("status update", s.handleModuleStatusUpdate)
	// Get initial states.
	for _, stateUpdate := range s.instance.GetStates() {
		s.states[stateUpdate.Module] = stateUpdate
		s.deriveNotificationsFromStateUpdate(stateUpdate)
	}

	return nil
}

// Stop stops the module.
func (s *Status) Stop() error {
	return nil
}

// AddToDebugInfo adds the system status to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	di.AddSection(
		fmt.Sprintf("Status: %s", netenv.GetOnlineStatus()),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		fmt.Sprintf("OnlineStatus:          %s", netenv.GetOnlineStatus()),
		"CaptivePortal:         "+netenv.GetCaptivePortal().URL,
	)
}

var (
	module     *Status
	shimLoaded atomic.Bool
)

// New returns a new status module.
func New(instance instance) (*Status, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Status")
	module = &Status{
		mgr:           m,
		instance:      instance,
		triggerUpdate: make(chan struct{}, 1),
		states:        make(map[string]mgr.StateUpdate),
		notifications: make(map[string]map[string]*notifications.Notification),
	}

	return module, nil
}

type instance interface {
	NetEnv() *netenv.NetEnv
	GetStates() []mgr.StateUpdate
	AddStatesCallback(callbackName string, callback mgr.EventCallbackFunc[mgr.StateUpdate])
}
