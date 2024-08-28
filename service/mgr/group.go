package mgr

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

var (
	// ErrUnsuitableGroupState is returned when an operation cannot be executed due to an unsuitable state.
	ErrUnsuitableGroupState = errors.New("unsuitable group state")

	// ErrInvalidGroupState is returned when a group is in an invalid state and cannot be recovered.
	ErrInvalidGroupState = errors.New("invalid group state")

	// ErrExecuteCmdLineOp is returned when modules are created, but request
	// execution of a (somewhere else set) command line operation instead.
	ErrExecuteCmdLineOp = errors.New("command line operation execution requested")
)

const (
	groupStateOff int32 = iota
	groupStateStarting
	groupStateRunning
	groupStateStopping
	groupStateInvalid
)

//nolint:goconst
func groupStateToString(state int32) string {
	switch state {
	case groupStateOff:
		return "off"
	case groupStateStarting:
		return "starting"
	case groupStateRunning:
		return "running"
	case groupStateStopping:
		return "stopping"
	case groupStateInvalid:
		return "invalid"
	}

	return "unknown"
}

// Group describes a group of modules.
type Group struct {
	modules []*groupModule

	state atomic.Int32
}

type groupModule struct {
	module Module
	mgr    *Manager
}

// Module is an manage-able instance of some component.
type Module interface {
	Manager() *Manager
	Start() error
	Stop() error
}

// NewGroup returns a new group of modules.
func NewGroup(modules ...Module) *Group {
	// Create group.
	g := &Group{
		modules: make([]*groupModule, 0, len(modules)),
	}

	// Initialize groups modules.
	for _, m := range modules {
		g.Add(m)
	}

	return g
}

// Add validates the given module and adds it to the group, if all requirements are met.
// Not safe for concurrent use with any other method.
// All modules must be added before anything else is done with the group.
func (g *Group) Add(m Module) {
	mgr := m.Manager()

	// Check module.
	switch {
	case m == nil:
		// Skip nil values to allow for cleaner code.
		return
	case reflect.ValueOf(m).IsNil():
		// If nil values are given via a struct, they are will be interfaces to a
		// nil type. Ignore these too.
		return
	case mgr == nil:
		// Ignore modules that do not return a manager.
		return
	case mgr.Name() == "":
		// Force name if none is set.
		// TODO: Unsafe if module is already logging, etc.
		mgr.setName(makeModuleName(m))
	}

	// Add module to group.
	g.modules = append(g.modules, &groupModule{
		module: m,
		mgr:    mgr,
	})
}

// Start starts all modules in the group in the defined order.
// If a module fails to start, itself and all previous modules
// will be stopped in the reverse order.
func (g *Group) Start() error {
	// Check group state.
	switch g.state.Load() {
	case groupStateRunning:
		// Already running.
		return nil
	case groupStateInvalid:
		// Something went terribly wrong, cannot recover from here.
		return fmt.Errorf("%w: cannot recover", ErrInvalidGroupState)
	default:
		if !g.state.CompareAndSwap(groupStateOff, groupStateStarting) {
			return fmt.Errorf("%w: group is not off, state: %s", ErrUnsuitableGroupState, groupStateToString(g.state.Load()))
		}
	}

	// Start modules.
	for i, m := range g.modules {
		m.mgr.Debug("starting")
		startTime := time.Now()

		err := m.mgr.Do("start module "+m.mgr.name, func(_ *WorkerCtx) error {
			return m.module.Start() //nolint:scopelint // Execution is synchronous.
		})
		if err != nil {
			m.mgr.Error(
				"failed to start",
				"err", err,
				"time", time.Since(startTime),
			)
			if !g.stopFrom(i) {
				g.state.Store(groupStateInvalid)
			} else {
				g.state.Store(groupStateOff)
			}
			return fmt.Errorf("failed to start %s: %w", m.mgr.name, err)
		}
		m.mgr.Info("started", "time", time.Since(startTime))
	}

	g.state.Store(groupStateRunning)
	return nil
}

// Stop stops all modules in the group in the reverse order.
func (g *Group) Stop() error {
	// Check group state.
	switch g.state.Load() {
	case groupStateOff:
		// Already stopped.
		return nil
	case groupStateInvalid:
		// Something went terribly wrong, cannot recover from here.
		return fmt.Errorf("%w: cannot recover", ErrInvalidGroupState)
	default:
		if !g.state.CompareAndSwap(groupStateRunning, groupStateStopping) {
			return fmt.Errorf("%w: group is not running, state: %s", ErrUnsuitableGroupState, groupStateToString(g.state.Load()))
		}
	}

	// Stop modules.
	if !g.stopFrom(len(g.modules) - 1) {
		g.state.Store(groupStateInvalid)
		return errors.New("failed to stop")
	}

	g.state.Store(groupStateOff)
	return nil
}

func (g *Group) stopFrom(index int) (ok bool) {
	ok = true

	// Stop modules.
	for i := index; i >= 0; i-- {
		m := g.modules[i]
		m.mgr.Debug("stopping")
		startTime := time.Now()

		err := m.mgr.Do("stop module "+m.mgr.name, func(_ *WorkerCtx) error {
			return m.module.Stop()
		})
		if err != nil {
			m.mgr.Error(
				"failed to stop",
				"err", err,
				"time", time.Since(startTime),
			)
			ok = false
		}
		m.mgr.Cancel()
		if m.mgr.WaitForWorkers(0) {
			m.mgr.Info("stopped", "time", time.Since(startTime))
		} else {
			ok = false
			m.mgr.Error(
				"failed to stop",
				"err", "timed out",
				"workerCnt", m.mgr.workerCnt.Load(),
				"time", time.Since(startTime),
			)
		}
	}

	// Reset modules.
	if !ok {
		// Stopping failed somewhere, reset anyway after a short wait.
		// This will be very uncommon and can help to mitigate race conditions in these events.
		time.Sleep(time.Second)
	}
	for _, m := range g.modules {
		m.mgr.Reset()
	}

	return ok
}

// Ready returns whether all modules in the group have been started and are still running.
func (g *Group) Ready() bool {
	return g.state.Load() == groupStateRunning
}

// GetStates returns the current states of all group modules.
func (g *Group) GetStates() []StateUpdate {
	updates := make([]StateUpdate, 0, len(g.modules))
	for _, gm := range g.modules {
		if stateful, ok := gm.module.(StatefulModule); ok {
			updates = append(updates, stateful.States().Export())
		}
	}
	return updates
}

// AddStatesCallback adds the given callback function to all group modules that
// expose a state manager at States().
func (g *Group) AddStatesCallback(callbackName string, callback EventCallbackFunc[StateUpdate]) {
	for _, gm := range g.modules {
		if stateful, ok := gm.module.(StatefulModule); ok {
			stateful.States().AddCallback(callbackName, callback)
		}
	}
}

// Modules returns a copy of the modules.
func (g *Group) Modules() []Module {
	copied := make([]Module, 0, len(g.modules))
	for _, gm := range g.modules {
		copied = append(copied, gm.module)
	}
	return copied
}

// RunModules is a simple wrapper function to start modules and stop them again
// when the given context is canceled.
func RunModules(ctx context.Context, modules ...Module) error {
	g := NewGroup(modules...)

	// Start module.
	if err := g.Start(); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	// Stop module when context is canceled.
	<-ctx.Done()
	return g.Stop()
}

func makeModuleName(m Module) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", m), "*")
}
