package mgr

import (
	"slices"
	"sync"
	"time"
)

// StateMgr is a simple state manager.
type StateMgr struct {
	states     []State
	statesLock sync.Mutex

	statesEventMgr *EventMgr[StateUpdate]

	mgr *Manager
}

// State describes the state of a manager or module.
type State struct {
	ID      string    // Required.
	Name    string    // Required.
	Message string    // Optional.
	Type    StateType // Optional.
	Time    time.Time // Optional, will be set to current time if not set.
	Data    any       // Optional.
}

// StateType defines commonly used states.
type StateType string

// State Types.
const (
	StateTypeUndefined = ""
	StateTypeHint      = "hint"
	StateTypeWarning   = "warning"
	StateTypeError     = "error"
)

// StateUpdate is used to update others about a state change.
type StateUpdate struct {
	Name   string
	States []State
}

// NewStateMgr returns a new event manager.
// It is easiest used as a public field on a struct,
// so that others can simply Subscribe() oder AddCallback().
func NewStateMgr(mgr *Manager) *StateMgr {
	return &StateMgr{
		statesEventMgr: NewEventMgr[StateUpdate]("state update", mgr),
		mgr:            mgr,
	}
}

// Add adds a state.
// If a state with the same ID already exists, it is replaced.
func (m *StateMgr) Add(s State) {
	m.statesLock.Lock()
	defer m.statesLock.Unlock()

	if s.Time.IsZero() {
		s.Time = time.Now()
	}

	// Update or add state.
	index := slices.IndexFunc[[]State, State](m.states, func(es State) bool {
		return es.ID == s.ID
	})
	if index > 0 {
		m.states[index] = s
	} else {
		m.states = append(m.states, s)
	}

	m.statesEventMgr.Submit(m.Export())
}

// Remove removes the state with the given ID.
func (m *StateMgr) Remove(id string) {
	m.statesLock.Lock()
	defer m.statesLock.Unlock()

	slices.DeleteFunc[[]State, State](m.states, func(s State) bool {
		return s.ID == id
	})

	m.statesEventMgr.Submit(m.Export())
}

// Clear removes all states.
func (m *StateMgr) Clear() {
	m.statesLock.Lock()
	defer m.statesLock.Unlock()

	m.states = nil

	m.statesEventMgr.Submit(m.Export())
}

// Export returns the current states.
func (m *StateMgr) Export() StateUpdate {
	m.statesLock.Lock()
	defer m.statesLock.Unlock()

	name := ""
	if m.mgr != nil {
		name = m.mgr.name
	}

	return StateUpdate{
		Name:   name,
		States: slices.Clone(m.states),
	}
}

// Subscribe subscribes to state update events.
func (m *StateMgr) Subscribe(subscriberName string, chanSize int) *EventSubscription[StateUpdate] {
	return m.statesEventMgr.Subscribe(subscriberName, chanSize)
}

// AddCallback adds a callback to state update events.
func (m *StateMgr) AddCallback(callbackName string, callback EventCallbackFunc[StateUpdate]) {
	m.statesEventMgr.AddCallback(callbackName, callback)
}
