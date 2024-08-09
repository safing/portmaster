package mgr

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ExtendedGroup extends the group with additional helpful functionality.
type ExtendedGroup struct {
	*Group

	ensureCtx    context.Context
	ensureCancel context.CancelFunc
	ensureLock   sync.Mutex
}

// NewExtendedGroup returns a new extended group.
func NewExtendedGroup(modules ...Module) *ExtendedGroup {
	return UpgradeGroup(NewGroup(modules...))
}

// UpgradeGroup upgrades a regular group to an extended group.
func UpgradeGroup(g *Group) *ExtendedGroup {
	return &ExtendedGroup{
		Group:        g,
		ensureCancel: func() {},
	}
}

// EnsureStartedWorker tries to start the group until it succeeds or fails permanently.
func (eg *ExtendedGroup) EnsureStartedWorker(wCtx *WorkerCtx) error {
	// Setup worker.
	var ctx context.Context
	func() {
		eg.ensureLock.Lock()
		defer eg.ensureLock.Unlock()
		eg.ensureCancel()
		eg.ensureCtx, eg.ensureCancel = context.WithCancel(wCtx.Ctx())
		ctx = eg.ensureCtx
	}()

	for {
		err := eg.Group.Start()
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrInvalidGroupState):
			wCtx.Debug("group start delayed", "err", err)
		default:
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(1 * time.Second):
		}
	}
}

// EnsureStoppedWorker tries to stop the group until it succeeds or fails permanently.
func (eg *ExtendedGroup) EnsureStoppedWorker(wCtx *WorkerCtx) error {
	// Setup worker.
	var ctx context.Context
	func() {
		eg.ensureLock.Lock()
		defer eg.ensureLock.Unlock()
		eg.ensureCancel()
		eg.ensureCtx, eg.ensureCancel = context.WithCancel(wCtx.Ctx())
		ctx = eg.ensureCtx
	}()

	for {
		err := eg.Group.Stop()
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrInvalidGroupState):
			wCtx.Debug("group stop delayed", "err", err)
		default:
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(1 * time.Second):
		}
	}
}
