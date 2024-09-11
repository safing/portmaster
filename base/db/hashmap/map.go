package hashmap

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mycoria/mycoria/mgr"
	"github.com/safing/portmaster/base/db"
	"github.com/safing/portmaster/base/db/query"
)

// HashMapDB stores database records in a Go map.
type HashMapDB struct {
	m    *mgr.Manager
	name string

	db     map[string]*HashMapRecord
	dbLock sync.RWMutex

	subs *db.SubscriptionPlugin

	// stopped simulates the database being unavailable, so that it behaves more
	// like a real database for testing.
	stopped atomic.Bool
}

// New creates a new hashmap database.
func New(name string) *HashMapDB {
	m := mgr.New(name)
	hm := &HashMapDB{
		name: name,
		m:    m,
		db:   make(map[string]*HashMapRecord),
		subs: db.NewSubscriptionPlugin(db.DefaultSubscriptionTimeout),
	}
	hm.stopped.Store(true)
	return hm
}

// Manager returns the module manager.
func (hm *HashMapDB) Manager() *mgr.Manager {
	return hm.m
}

// Start starts the module.
func (hm *HashMapDB) Start() error {
	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	hm.stopped.Store(false)
	return nil
}

// Stop stops the module.
func (hm *HashMapDB) Stop() error {
	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	hm.subs.FinishAll(nil)
	clear(hm.db)
	hm.stopped.Store(true)
	return nil
}

// Exists checks whether a record with the given key exists in the database.
func (hm *HashMapDB) Exists(key string) (bool, error) {
	if hm.stopped.Load() {
		return false, db.ErrStopped
	}

	hm.dbLock.RLock()
	defer hm.dbLock.RUnlock()

	_, ok := hm.db[key]
	return ok, nil
}

// Get fetches the record with the given key from the database.
func (hm *HashMapDB) Get(key string) (db.Record, error) {
	if hm.stopped.Load() {
		return nil, db.ErrStopped
	}

	hm.dbLock.RLock()
	defer hm.dbLock.RUnlock()

	r, ok := hm.db[key]
	if !ok {
		return nil, db.ErrNotFound
	}

	return r, nil
}

// Put stores a record in the database.
func (hm *HashMapDB) Put(r db.Record) error {
	if hm.stopped.Load() {
		return db.ErrStopped
	}
	if r == nil {
		return nil
	}

	// Create a hashmap record.
	hmr := &HashMapRecord{
		key:        r.Key(),
		created:    r.Created(),
		updated:    time.Now(),
		permission: r.Permission(),
		object:     r.Object(),
		format:     r.Format(),
		data:       r.Data(),
	}

	// Check if the given record is valid
	switch {
	case hmr.object == nil && hmr.data == nil:
		return db.ErrInvalidRecord
	case hmr.format == 0:
		return db.ErrInvalidRecord
	}

	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	hm.db[hmr.key] = hmr
	hm.subs.Submit(hm.m.Ctx(), hmr)
	return nil
}

// BatchPut returns a put function to quickly add many records at once to the database.
// The caller must call put(nil) when done.
func (hm *HashMapDB) BatchPut() (put func(db.Record) error, err error) {
	if hm.stopped.Load() {
		return nil, db.ErrStopped
	}

	// TODO: Can we make this more safe in case someone forgets to call put(nil)
	// or the worker that should do it panics?
	return hm.Put, nil
}

// Delete removes the record with the given key from the database.
func (hm *HashMapDB) Delete(key string) error {
	if hm.stopped.Load() {
		return db.ErrStopped
	}

	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	// Check for existing record.
	existing, ok := hm.db[key]
	if ok {
		// Submit to subscribers.
		hm.subs.Submit(hm.m.Ctx(), db.MakeDeletedRecord(existing))

		// Delete from map.
		delete(hm.db, key)
	}

	return nil
}

// BatchDelete removes all records that match the given query from the database.
func (hm *HashMapDB) BatchDelete(q *query.Query) (int, error) {
	if hm.stopped.Load() {
		return 0, db.ErrStopped
	}

	if err := q.Check(); err != nil {
		return 0, err
	}

	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	var deleted int
	for key, r := range hm.db {
		if q.Matches(r) {
			// Submit to subscribers.
			hm.subs.Submit(hm.m.Ctx(), db.MakeDeletedRecord(r))

			// Delete from map and count.
			delete(hm.db, key)
			deleted++
		}
	}

	return deleted, nil
}

func (hm *HashMapDB) Query(q *query.Query, queueSize int) (*db.Iterator, error) {
	if hm.stopped.Load() {
		return nil, db.ErrStopped
	}

	// Start query runner.
	qr, err := db.NewQueryRunner(q, queueSize, db.DefaultQueryTimeout)
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}
	hm.m.Go("query runner", func(w *mgr.WorkerCtx) error {
		defer qr.Finish(nil)

		hm.dbLock.RLock()
		defer hm.dbLock.RUnlock()

		// Submit all record to the query runner for processing.
		for _, record := range hm.db {
			done := qr.Submit(w.Ctx(), record)
			if done {
				return nil
			}
		}
		return nil
	})

	return qr.Iterator(), nil
}

func (hm *HashMapDB) Subscribe(q *query.Query, queueSize int) (*db.Subscription, error) {
	if hm.stopped.Load() {
		return nil, db.ErrStopped
	}

	return hm.subs.Subscribe(q, queueSize)
}
