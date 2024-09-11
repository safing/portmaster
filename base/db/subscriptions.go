package db

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/safing/portmaster/base/db/query"
)

type SubscriptionPlugin struct {
	lock sync.RWMutex
	subs []*Subscription

	timeout time.Duration
}

type Subscription struct {
	Iterator

	q *query.Query
}

func NewSubscriptionPlugin(timeout time.Duration) *SubscriptionPlugin {
	return &SubscriptionPlugin{
		subs:    make([]*Subscription, 0, 16),
		timeout: timeout,
	}
}

// Subscribe subscribes to updates to the given query.
func (sp *SubscriptionPlugin) Subscribe(q *query.Query, queueSize int) (*Subscription, error) {
	// Check subscription query.
	if err := q.Check(); err != nil {
		return nil, err
	}

	// Create new subscription.
	sub := &Subscription{
		Iterator: Iterator{
			Next: make(chan Record, queueSize),
			Done: make(chan struct{}),
		},
		q: q,
	}

	sp.lock.Lock()
	defer sp.lock.Unlock()

	// Clear finished subscriptions before adding a new one.
	sp.subs = slices.DeleteFunc(sp.subs, func(sub *Subscription) bool {
		if sub.IsDone() {
			sub.finish(nil)
			return true
		}
		return false
	})

	// Add and return new subscription.
	sp.subs = append(sp.subs, sub)
	return sub, nil
}

func (sp *SubscriptionPlugin) Submit(ctx context.Context, record Record) (done bool) {
	if sp == nil {
		return
	}

	sp.lock.RLock()
	defer sp.lock.RUnlock()

subs:
	for _, sub := range sp.subs {
		switch {
		case sub.IsDone():
			continue subs
		case !sub.q.Matches(record):
			continue subs
		}

		select {
		case sub.Next <- record:
			// Continue finding next record.
			return false
		case <-sub.Done:
			return true
		case <-ctx.Done():
			sub.finish(ErrCanceled)
			return true

		default:
			select {
			case sub.Next <- record:
				// Continue finding next record.
				return false
			case <-sub.Done:
				return true
			case <-ctx.Done():
				sub.finish(ErrCanceled)
				return true

			case <-time.After(sp.timeout):
				sub.finish(ErrTimeout)
				return true
			}
		}
	}

	return false
}

// FinishAll finishes all subcriptions using the given error.
func (sp *SubscriptionPlugin) FinishAll(err error) {
	sp.lock.Lock()
	defer sp.lock.Unlock()

	for _, sub := range sp.subs {
		sub.finish(err)
	}

	clear(sp.subs)
}
