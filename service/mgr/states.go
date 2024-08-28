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
	// ID is a program-unique ID.
	// It must not only be unique within the StateMgr, but for the whole program,
	// as it may be re-used with related systems.
	// Required.
	ID string

	// Name is the name of the state.
	// This may also serve as a notification title.
	// Required.
	Name string

	// Message is a more detailed message about the state.
	// Optional.
	Message string

	// Type defines the type of the state.
	// Optional.
	Type StateType

	// Time is the time when the state was created or the originating incident occurred.
	// Optional, will be set to current time if not set.
	Time time.Time

	// Data can hold any additional data necessary for further processing of connected systems.
	// Optional.
	Data any
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

// Severity returns a number representing the gravity of the state for ordering.
func (st StateType) Severity() int {
	switch st {
	case StateTypeUndefined:
		return 0
	case StateTypeHint:
		return 1
	case StateTypeWarning:
		return 2
	case StateTypeError:
		return 3
	default:
		return 0
	}
}

// StateUpdate is used to update others about a state change.
type StateUpdate struct {
	Module string
	States []State
}

// StatefulModule is used for interface checks on modules.
type StatefulModule interface {
	States() *StateMgr
}

// NewStateMgr returns a new state manager.
func NewStateMgr(mgr *Manager) *StateMgr {
	return &StateMgr{
		statesEventMgr: NewEventMgr[StateUpdate]("state update", mgr),
		mgr:            mgr,
	}
}

// NewStateMgr returns a new state manager.
func (m *Manager) NewStateMgr() *StateMgr {
	return NewStateMgr(m)
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
	index := slices.IndexFunc(m.states, func(es State) bool {
		return es.ID == s.ID
	})
	if index >= 0 {
		m.states[index] = s
	} else {
		m.states = append(m.states, s)
	}

	m.statesEventMgr.Submit(m.export())
}

// Remove removes the state with the given ID.
func (m *StateMgr) Remove(id string) {
	m.statesLock.Lock()
	defer m.statesLock.Unlock()

	// Quick check if slice is empty.
	// It is a common pattern to remove a state when no error was encountered at
	// a critical operation. This means that StateMgr.Remove will be called often.
	if len(m.states) == 0 {
		return
	}

	var entryRemoved bool
	m.states = slices.DeleteFunc(m.states, func(s State) bool {
		if s.ID == id {
			entryRemoved = true
			return true
		}
		return false
	})

	if entryRemoved {
		m.statesEventMgr.Submit(m.export())
	}
}

// Clear removes all states.
func (m *StateMgr) Clear() {
	m.statesLock.Lock()
	m.states = nil
	m.statesLock.Unlock()

	// Submit event without lock, because callbacks might come back to change states.
	defer m.statesEventMgr.Submit(m.Export())
}

// Export returns the current states.
func (m *StateMgr) Export() StateUpdate {
	m.statesLock.Lock()
	defer m.statesLock.Unlock()

	return m.export()
}

// export returns the current states.
func (m *StateMgr) export() StateUpdate {
	name := ""
	if m.mgr != nil {
		name = m.mgr.name
	}

	return StateUpdate{
		Module: name,
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
