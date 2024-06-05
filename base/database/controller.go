package database

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

// A Controller takes care of all the extra database logic.
type Controller struct {
	database     *Database
	storage      storage.Interface
	shadowDelete bool

	hooksLock sync.RWMutex
	hooks     []*RegisteredHook

	subscriptionLock sync.RWMutex
	subscriptions    []*Subscription
}

// newController creates a new controller for a storage.
func newController(database *Database, storageInt storage.Interface, shadowDelete bool) *Controller {
	return &Controller{
		database:     database,
		storage:      storageInt,
		shadowDelete: shadowDelete,
	}
}

// ReadOnly returns whether the storage is read only.
func (c *Controller) ReadOnly() bool {
	return c.storage.ReadOnly()
}

// Injected returns whether the storage is injected.
func (c *Controller) Injected() bool {
	return c.storage.Injected()
}

// Get returns the record with the given key.
func (c *Controller) Get(key string) (record.Record, error) {
	if shuttingDown.IsSet() {
		return nil, ErrShuttingDown
	}

	if err := c.runPreGetHooks(key); err != nil {
		return nil, err
	}

	r, err := c.storage.Get(key)
	if err != nil {
		// replace not found error
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	r.Lock()
	defer r.Unlock()

	r, err = c.runPostGetHooks(r)
	if err != nil {
		return nil, err
	}

	if !r.Meta().CheckValidity() {
		return nil, ErrNotFound
	}

	return r, nil
}

// GetMeta returns the metadata of the record with the given key.
func (c *Controller) GetMeta(key string) (*record.Meta, error) {
	if shuttingDown.IsSet() {
		return nil, ErrShuttingDown
	}

	var m *record.Meta
	var err error
	if metaDB, ok := c.storage.(storage.MetaHandler); ok {
		m, err = metaDB.GetMeta(key)
		if err != nil {
			// replace not found error
			if errors.Is(err, storage.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	} else {
		r, err := c.storage.Get(key)
		if err != nil {
			// replace not found error
			if errors.Is(err, storage.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		m = r.Meta()
	}

	if !m.CheckValidity() {
		return nil, ErrNotFound
	}

	return m, nil
}

// Put saves a record in the database, executes any registered
// pre-put hooks and finally send an update to all subscribers.
// The record must be locked and secured from concurrent access
// when calling Put().
func (c *Controller) Put(r record.Record) (err error) {
	if shuttingDown.IsSet() {
		return ErrShuttingDown
	}

	if c.ReadOnly() {
		return ErrReadOnly
	}

	r, err = c.runPrePutHooks(r)
	if err != nil {
		return err
	}

	if !c.shadowDelete && r.Meta().IsDeleted() {
		// Immediate delete.
		err = c.storage.Delete(r.DatabaseKey())
	} else {
		// Put or shadow delete.
		r, err = c.storage.Put(r)
	}

	if err != nil {
		return err
	}

	if r == nil {
		return errors.New("storage returned nil record after successful put operation")
	}

	c.notifySubscribers(r)

	return nil
}

// PutMany stores many records in the database. It does not
// process any hooks or update subscriptions. Use with care!
func (c *Controller) PutMany() (chan<- record.Record, <-chan error) {
	if shuttingDown.IsSet() {
		errs := make(chan error, 1)
		errs <- ErrShuttingDown
		return make(chan record.Record), errs
	}

	if c.ReadOnly() {
		errs := make(chan error, 1)
		errs <- ErrReadOnly
		return make(chan record.Record), errs
	}

	if batcher, ok := c.storage.(storage.Batcher); ok {
		return batcher.PutMany(c.shadowDelete)
	}

	errs := make(chan error, 1)
	errs <- ErrNotImplemented
	return make(chan record.Record), errs
}

// Query executes the given query on the database.
func (c *Controller) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	if shuttingDown.IsSet() {
		return nil, ErrShuttingDown
	}

	it, err := c.storage.Query(q, local, internal)
	if err != nil {
		return nil, err
	}

	return it, nil
}

// PushUpdate pushes a record update to subscribers.
// The caller must hold the record's lock when calling
// PushUpdate.
func (c *Controller) PushUpdate(r record.Record) {
	if c != nil {
		if shuttingDown.IsSet() {
			return
		}

		c.notifySubscribers(r)
	}
}

func (c *Controller) addSubscription(sub *Subscription) {
	if shuttingDown.IsSet() {
		return
	}

	c.subscriptionLock.Lock()
	defer c.subscriptionLock.Unlock()

	c.subscriptions = append(c.subscriptions, sub)
}

// Maintain runs the Maintain method on the storage.
func (c *Controller) Maintain(ctx context.Context) error {
	if shuttingDown.IsSet() {
		return ErrShuttingDown
	}

	if maintainer, ok := c.storage.(storage.Maintainer); ok {
		return maintainer.Maintain(ctx)
	}
	return nil
}

// MaintainThorough runs the MaintainThorough method on the
// storage.
func (c *Controller) MaintainThorough(ctx context.Context) error {
	if shuttingDown.IsSet() {
		return ErrShuttingDown
	}

	if maintainer, ok := c.storage.(storage.Maintainer); ok {
		return maintainer.MaintainThorough(ctx)
	}
	return nil
}

// MaintainRecordStates runs the record state lifecycle
// maintenance on the storage.
func (c *Controller) MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time) error {
	if shuttingDown.IsSet() {
		return ErrShuttingDown
	}

	return c.storage.MaintainRecordStates(ctx, purgeDeletedBefore, c.shadowDelete)
}

// Purge deletes all records that match the given query.
// It returns the number of successful deletes and an error.
func (c *Controller) Purge(ctx context.Context, q *query.Query, local, internal bool) (int, error) {
	if shuttingDown.IsSet() {
		return 0, ErrShuttingDown
	}

	if purger, ok := c.storage.(storage.Purger); ok {
		return purger.Purge(ctx, q, local, internal, c.shadowDelete)
	}

	return 0, ErrNotImplemented
}

// Shutdown shuts down the storage.
func (c *Controller) Shutdown() error {
	return c.storage.Shutdown()
}

// notifySubscribers notifies all subscribers that are interested
// in r. r must be locked when calling notifySubscribers.
// Any subscriber that is not blocking on it's feed channel will
// be skipped.
func (c *Controller) notifySubscribers(r record.Record) {
	c.subscriptionLock.RLock()
	defer c.subscriptionLock.RUnlock()

	for _, sub := range c.subscriptions {
		if r.Meta().CheckPermission(sub.local, sub.internal) && sub.q.Matches(r) {
			select {
			case sub.Feed <- r:
			default:
			}
		}
	}
}

func (c *Controller) runPreGetHooks(key string) error {
	c.hooksLock.RLock()
	defer c.hooksLock.RUnlock()

	for _, hook := range c.hooks {
		if !hook.h.UsesPreGet() {
			continue
		}

		if !hook.q.MatchesKey(key) {
			continue
		}

		if err := hook.h.PreGet(key); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) runPostGetHooks(r record.Record) (record.Record, error) {
	c.hooksLock.RLock()
	defer c.hooksLock.RUnlock()

	var err error
	for _, hook := range c.hooks {
		if !hook.h.UsesPostGet() {
			continue
		}

		if !hook.q.Matches(r) {
			continue
		}

		r, err = hook.h.PostGet(r)
		if err != nil {
			return nil, err
		}
	}

	return r, nil
}

func (c *Controller) runPrePutHooks(r record.Record) (record.Record, error) {
	c.hooksLock.RLock()
	defer c.hooksLock.RUnlock()

	var err error
	for _, hook := range c.hooks {
		if !hook.h.UsesPrePut() {
			continue
		}

		if !hook.q.Matches(r) {
			continue
		}

		r, err = hook.h.PrePut(r)
		if err != nil {
			return nil, err
		}
	}

	return r, nil
}
