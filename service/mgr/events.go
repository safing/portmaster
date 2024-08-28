//nolint:structcheck,golint // TODO: Seems broken for generics.
package mgr

import (
	"slices"
	"sync"
	"sync/atomic"
)

// EventMgr is a simple event manager.
type EventMgr[T any] struct {
	name string
	mgr  *Manager
	lock sync.Mutex

	subs      []*EventSubscription[T]
	callbacks []*EventCallback[T]
}

// EventSubscription is a subscription to an event.
type EventSubscription[T any] struct {
	name     string
	events   chan T
	canceled atomic.Bool
}

// EventCallback is a registered callback to an event.
type EventCallback[T any] struct {
	name     string
	callback EventCallbackFunc[T]
	canceled atomic.Bool
}

// EventCallbackFunc defines the event callback function.
type EventCallbackFunc[T any] func(*WorkerCtx, T) (cancel bool, err error)

// NewEventMgr returns a new event manager.
// It is easiest used as a public field on a struct,
// so that others can simply Subscribe() oder AddCallback().
func NewEventMgr[T any](eventName string, mgr *Manager) *EventMgr[T] {
	return &EventMgr[T]{
		name: eventName,
		mgr:  mgr,
	}
}

// Subscribe subscribes to events.
// The received events are shared among all subscribers and callbacks.
// Be sure to apply proper concurrency safeguards, if applicable.
func (em *EventMgr[T]) Subscribe(subscriberName string, chanSize int) *EventSubscription[T] {
	em.lock.Lock()
	defer em.lock.Unlock()

	es := &EventSubscription[T]{
		name:   subscriberName,
		events: make(chan T, chanSize),
	}

	em.subs = append(em.subs, es)
	return es
}

// AddCallback adds a callback to executed on events.
// The received events are shared among all subscribers and callbacks.
// Be sure to apply proper concurrency safeguards, if applicable.
func (em *EventMgr[T]) AddCallback(callbackName string, callback EventCallbackFunc[T]) {
	em.lock.Lock()
	defer em.lock.Unlock()

	ec := &EventCallback[T]{
		name:     callbackName,
		callback: callback,
	}

	em.callbacks = append(em.callbacks, ec)
}

// Submit submits a new event.
func (em *EventMgr[T]) Submit(event T) {
	em.lock.Lock()
	defer em.lock.Unlock()

	var anyCanceled bool

	// Send to subscriptions.
	for _, sub := range em.subs {
		// Check if subscription was canceled.
		if sub.canceled.Load() {
			anyCanceled = true
			continue
		}

		// Submit via channel.
		select {
		case sub.events <- event:
		default:
			if em.mgr != nil {
				em.mgr.Warn(
					"event subscription channel overflow",
					"event", em.name,
					"subscriber", sub.name,
				)
			}
		}
	}

	// Run callbacks.
	for _, ec := range em.callbacks {
		// Check if callback was canceled.
		if ec.canceled.Load() {
			anyCanceled = true
			continue
		}

		// Execute callback.
		var (
			cancel bool
			err    error
		)
		if em.mgr != nil {
			// Prefer executing in worker.
			name := "event " + em.name + " callback " + ec.name
			em.mgr.Go(name, func(w *WorkerCtx) error {
				cancel, err = ec.callback(w, event)
				// Handle error and cancelation.
				if err != nil {
					w.Warn(
						"event callback failed",
						"event", em.name,
						"callback", ec.name,
						"err", err,
					)
				}
				if cancel {
					ec.canceled.Store(true)
				}
				return nil
			})
		} else {
			cancel, err = ec.callback(nil, event)
			// Handle error and cancelation.
			if err != nil && em.mgr != nil {
				em.mgr.Warn(
					"event callback failed",
					"event", em.name,
					"callback", ec.name,
					"err", err,
				)
			}
			if cancel {
				ec.canceled.Store(true)
				anyCanceled = true
			}
		}
	}

	// If any canceled subscription/callback was seen, clean the slices.
	if anyCanceled {
		em.clean()
	}
}

// clean removes all canceled subscriptions and callbacks.
func (em *EventMgr[T]) clean() {
	em.subs = slices.DeleteFunc[[]*EventSubscription[T], *EventSubscription[T]](em.subs, func(es *EventSubscription[T]) bool {
		return es.canceled.Load()
	})
	em.callbacks = slices.DeleteFunc[[]*EventCallback[T], *EventCallback[T]](em.callbacks, func(ec *EventCallback[T]) bool {
		return ec.canceled.Load()
	})
}

// Events returns a read channel for the events.
// The received events are shared among all subscribers and callbacks.
// Be sure to apply proper concurrency safeguards, if applicable.
func (es *EventSubscription[T]) Events() <-chan T {
	return es.events
}

// Cancel cancels the subscription.
// The events channel is not closed, but will not receive new events.
func (es *EventSubscription[T]) Cancel() {
	es.canceled.Store(true)
}

// Done returns whether the event subscription has been canceled.
func (es *EventSubscription[T]) Done() bool {
	return es.canceled.Load()
}
