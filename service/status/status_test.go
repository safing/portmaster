package status

import (
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
)

var notificationsSetupOnce sync.Once

type statusTestInstance struct {
	states *mgr.StateMgr
}

func (i *statusTestInstance) NetEnv() *netenv.NetEnv { return nil }
func (i *statusTestInstance) GetStates() []mgr.StateUpdate {
	return []mgr.StateUpdate{i.states.Export()}
}

func (i *statusTestInstance) AddStatesCallback(callbackName string, callback mgr.EventCallbackFunc[mgr.StateUpdate]) {
	i.states.AddCallback(callbackName, callback)
}

// TestModuleStatusConvergesToLatestState checks that the status module ends up
// with the latest module state, even though state update callbacks are
// executed in separate goroutines and may therefore run out of order.
// A state that was added and immediately removed again (a common pattern for
// error states) must not stay in the system status, nor its notification.
//
//nolint:paralleltest // Uses the global notification storage.
func TestModuleStatusConvergesToLatestState(t *testing.T) {
	notificationsSetupOnce.Do(func() {
		_, err := notifications.New(struct{}{})
		if err != nil {
			t.Fatalf("failed to create notifications module: %s", err)
		}
	})

	moduleMgr := mgr.New("TestModule")
	inst := &statusTestInstance{states: mgr.NewStateMgr(moduleMgr)}
	statusMgr := mgr.New("Status")
	s := &Status{
		mgr:           statusMgr,
		instance:      inst,
		triggerUpdate: make(chan struct{}, 1),
		states:        make(map[string]mgr.StateUpdate),
		notifications: make(map[string]map[string]*notifications.Notification),
	}
	inst.AddStatesCallback("status update", s.handleModuleStatusUpdate)

	const stateID = "test:transient-error"
	for range 300 {
		inst.states.Add(mgr.State{
			ID:      stateID,
			Name:    "Transient error",
			Message: "Transient error",
			Type:    mgr.StateTypeError,
		})
		inst.states.Remove(stateID)
	}
	if !moduleMgr.WaitForWorkers(5 * time.Second) {
		t.Fatal("state update callbacks did not finish")
	}

	s.statesLock.Lock()
	got := s.states[moduleMgr.Name()]
	s.statesLock.Unlock()
	if len(got.States) != 0 {
		t.Errorf("status kept stale module states: %+v", got.States)
	}
	if n := notifications.Get(stateID); n != nil {
		t.Errorf("notification for removed state %q still shown", stateID)
	}
}
