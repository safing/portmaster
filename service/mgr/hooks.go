package mgr

import (
	"slices"
	"sync"
)

// HookMgr manages synchronous hooks.
// Unlike EventMgr, Invoke blocks until every registered hook has returned,
// making it suitable for pre-operation hooks where the caller must wait for all
// hooks to complete.
type HookMgr[T any] struct {
	name string
	mgr  *Manager
	lock sync.RWMutex

	callbacks []*EventCallback[T]
}

// NewHookMgr returns a new hook manager.
func NewHookMgr[T any](name string, mgr *Manager) *HookMgr[T] {
	return &HookMgr[T]{
		name: name,
		mgr:  mgr,
	}
}

// AddHook registers a hook that will be called synchronously by Invoke.
// Use the same EventCallbackFunc signature as EventMgr:
//   - returning cancel=true removes the hook from future invocations.
//   - returning a non-nil error causes Invoke to stop and return that error.
func (cm *HookMgr[T]) AddHook(hookName string, hook EventCallbackFunc[T]) {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	cm.callbacks = append(cm.callbacks, &EventCallback[T]{
		name:     hookName,
		callback: hook,
	})
}

// Invoke calls all registered hooks synchronously in registration order.
// It blocks until every hook has returned.
// If a hook returns an error, Invoke stops immediately and returns that error.
// Hooks that return cancel=true are removed from future invocations.
func (cm *HookMgr[T]) Invoke(wc *WorkerCtx, data T) error {
	cm.lock.RLock()
	snapshot := make([]*EventCallback[T], len(cm.callbacks))
	copy(snapshot, cm.callbacks)
	cm.lock.RUnlock()

	var anyCanceled bool
	for _, ec := range snapshot {
		if ec.canceled.Load() {
			anyCanceled = true
			continue
		}

		cancel, err := ec.callback(wc, data)
		if err != nil {
			if cm.mgr != nil {
				cm.mgr.Warn(
					"hook failed",
					"hook_mgr", cm.name,
					"hook", ec.name,
					"err", err,
				)
			}
			return err
		}
		if cancel {
			ec.canceled.Store(true)
			anyCanceled = true
		}
	}

	if anyCanceled {
		cm.lock.Lock()
		defer cm.lock.Unlock()
		cm.callbacks = slices.DeleteFunc(cm.callbacks, func(ec *EventCallback[T]) bool {
			return ec.canceled.Load()
		})
	}

	return nil
}
