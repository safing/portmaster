package mgr

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	groupStateOff int32 = iota
	groupStateStarting
	groupStateRunning
	groupStateStopping
	groupStateInvalid
)

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

	ctx       context.Context
	cancelCtx context.CancelFunc
	ctxLock   sync.Mutex

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
	g.initGroupContext()

	// Initialize groups modules.
	for _, m := range modules {
		// Skip non-values.
		switch {
		case m == nil:
			// Skip nil values to allow for cleaner code.
			continue
		case reflect.ValueOf(m).IsNil():
			// If nil values are given via a struct, they are will be interfaces to a
			// nil type. Ignore these too.
			continue
		}

		// Add module to group.
		g.modules = append(g.modules, &groupModule{
			module: m,
			mgr:    newManager(g.ctx, makeModuleName(m), "module"),
		})
	}

	return g
}

// Start starts all modules in the group in the defined order.
// If a module fails to start, itself and all previous modules
// will be stopped in the reverse order.
func (g *Group) Start() error {
	if !g.state.CompareAndSwap(groupStateOff, groupStateStarting) {
		return fmt.Errorf("group is not off, state: %s", groupStateToString(g.state.Load()))
	}

	g.initGroupContext()

	for i, m := range g.modules {
		m.mgr.Info("starting")
		startTime := time.Now()

		err := m.mgr.Do(m.mgr.name+" Start", func(_ *WorkerCtx) error {
			return m.module.Start()
		})
		if err != nil {
			if !g.stopFrom(i) {
				g.state.Store(groupStateInvalid)
			} else {
				g.state.Store(groupStateOff)
			}
			return fmt.Errorf("failed to start %s: %w", makeModuleName(m.module), err)
		}
		duration := time.Since(startTime)
		m.mgr.Info("started " + duration.String())
	}
	g.state.Store(groupStateRunning)
	return nil
}

// Stop stops all modules in the group in the reverse order.
func (g *Group) Stop() error {
	if !g.state.CompareAndSwap(groupStateRunning, groupStateStopping) {
		return fmt.Errorf("group is not running, state: %s", groupStateToString(g.state.Load()))
	}

	if !g.stopFrom(len(g.modules) - 1) {
		g.state.Store(groupStateInvalid)
		return errors.New("failed to stop")
	}

	g.state.Store(groupStateOff)
	return nil
}

func (g *Group) stopFrom(index int) (ok bool) {
	ok = true
	for i := index; i >= 0; i-- {
		m := g.modules[i]

		err := m.mgr.Do(m.mgr.name+" Stop", func(_ *WorkerCtx) error {
			return m.module.Stop()
		})
		if err != nil {
			m.mgr.Error("failed to stop", "err", err)
			ok = false
		}
		m.mgr.Cancel()
		if m.mgr.WaitForWorkers(0) {
			m.mgr.Info("stopped")
		} else {
			ok = false
			m.mgr.Error(
				"failed to stop",
				"err", "timed out",
				"workerCnt", m.mgr.workerCnt.Load(),
			)
		}
	}

	g.stopGroupContext()
	return
}

func (g *Group) initGroupContext() {
	g.ctxLock.Lock()
	defer g.ctxLock.Unlock()

	g.ctx, g.cancelCtx = context.WithCancel(context.Background())
}

func (g *Group) stopGroupContext() {
	g.ctxLock.Lock()
	defer g.ctxLock.Unlock()

	g.cancelCtx()
}

// Done returns the context Done channel.
func (g *Group) Done() <-chan struct{} {
	g.ctxLock.Lock()
	defer g.ctxLock.Unlock()

	return g.ctx.Done()
}

// IsDone checks whether the manager context is done.
func (g *Group) IsDone() bool {
	g.ctxLock.Lock()
	defer g.ctxLock.Unlock()

	return g.ctx.Err() != nil
}

// GetStatus returns the current Status of all group modules.
func (g *Group) GetStatus() []StateUpdate {
	updates := make([]StateUpdate, 0, len(g.modules))
	for _, gm := range g.modules {
		if stateful, ok := gm.module.(StatefulModule); ok {
			updates = append(updates, stateful.States().Export())
		}
	}
	return updates
}

// AddStatusCallback adds the given callback function to all group modules that
// expose a state manager at States().
func (g *Group) AddStatusCallback(callbackName string, callback EventCallbackFunc[StateUpdate]) {
	for _, gm := range g.modules {
		if stateful, ok := gm.module.(StatefulModule); ok {
			stateful.States().AddCallback(callbackName, callback)
		}
	}
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
