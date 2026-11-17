package hashmap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

// HashMap storage.
type HashMap struct {
	name   string
	db     map[string]record.Record
	dbLock sync.RWMutex
}

func init() {
	_ = storage.Register("hashmap", NewHashMap)
}

// NewHashMap creates a hashmap database.
func NewHashMap(name, location string) (storage.Interface, error) {
	return &HashMap{
		name: name,
		db:   make(map[string]record.Record),
	}, nil
}

// Get returns a database record.
func (hm *HashMap) Get(key string) (record.Record, error) {
	hm.dbLock.RLock()
	defer hm.dbLock.RUnlock()

	r, ok := hm.db[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return r, nil
}

// GetMeta returns the metadata of a database record.
func (hm *HashMap) GetMeta(key string) (*record.Meta, error) {
	// TODO: Replace with more performant variant.

	r, err := hm.Get(key)
	if err != nil {
		return nil, err
	}

	return r.Meta(), nil
}

// Put stores a record in the database.
func (hm *HashMap) Put(r record.Record) (record.Record, error) {
	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	hm.db[r.DatabaseKey()] = r
	return r, nil
}

// PutMany stores many records in the database.
// Every record is stored as soon as it is received, so a batch is not atomic
// and cannot be rolled back. The hashmap therefore does not implement
// storage.AbortableBatcher on purpose: when a batch is aborted (see
// database.Interface.PutMany), the records passed on before stay stored, and
// the batch result says so.
func (hm *HashMap) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()
	// we could lock for every record, but we want to have the same behaviour
	// as the other storage backends, especially for testing.

	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	// start handler
	go func() {
		for r := range batch {
			hm.batchPutOrDelete(shadowDelete, r)
		}
		errs <- nil
	}()

	return batch, errs
}

func (hm *HashMap) batchPutOrDelete(shadowDelete bool, r record.Record) {
	r.Lock()
	defer r.Unlock()

	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	if !shadowDelete && r.Meta().IsDeleted() {
		delete(hm.db, r.DatabaseKey())
	} else {
		hm.db[r.DatabaseKey()] = r
	}
}

// Delete deletes a record from the database.
func (hm *HashMap) Delete(key string) error {
	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	delete(hm.db, key)
	return nil
}

// Query returns a an iterator for the supplied query.
func (hm *HashMap) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	_, err := q.Check()
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	queryIter := iterator.New()

	go hm.queryExecutor(queryIter, q, local, internal)
	return queryIter, nil
}

func (hm *HashMap) queryExecutor(queryIter *iterator.Iterator, q *query.Query, local, internal bool) {
	// Collect the matching records under the lock, but feed them to the
	// consumer only after releasing it, so that a slow consumer does not
	// block writers (and, through a waiting writer, all other readers).
	// Only the record pointers are held, the records are not copied.
	matches := hm.queryMatches(q, local, internal)

	var err error

mapLoop:
	for _, record := range matches {
		select {
		case <-queryIter.Done:
			break mapLoop
		case queryIter.Next <- record:
		default:
			select {
			case <-queryIter.Done:
				break mapLoop
			case queryIter.Next <- record:
			case <-time.After(1 * time.Second):
				err = errors.New("query timeout")
				break mapLoop
			}
		}

	}

	queryIter.Finish(err)
}

// ReadOnly returns whether the database is read only.
func (hm *HashMap) ReadOnly() bool {
	return false
}

// Injected returns whether the database is injected.
func (hm *HashMap) Injected() bool {
	return false
}

// MaintainRecordStates maintains records states in the database.
func (hm *HashMap) MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time, shadowDelete bool) error {
	hm.dbLock.Lock()
	defer hm.dbLock.Unlock()

	now := time.Now().Unix()
	purgeThreshold := purgeDeletedBefore.Unix()

	for key, record := range hm.db {
		// check if context is cancelled
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		meta := record.Meta()
		switch {
		case meta.Deleted == 0 && meta.Expires > 0 && meta.Expires < now:
			if shadowDelete {
				// mark as deleted
				record.Lock()
				meta.Deleted = meta.Expires
				record.Unlock()

				continue
			}

			// Immediately delete expired entries if shadowDelete is disabled.
			fallthrough
		case meta.Deleted > 0 && (!shadowDelete || meta.Deleted < purgeThreshold):
			// delete from storage
			delete(hm.db, key)
		}
	}

	return nil
}

// Shutdown shuts down the database.
func (hm *HashMap) Shutdown() error {
	return nil
}

func (hm *HashMap) queryMatches(q *query.Query, local, internal bool) []record.Record {
	hm.dbLock.RLock()
	defer hm.dbLock.RUnlock()

	var matches []record.Record
	for key, r := range hm.db {
		if !q.MatchesKey(key) {
			continue
		}

		r.Lock()
		if q.MatchesRecord(r) &&
			r.Meta().CheckValidity() &&
			r.Meta().CheckPermission(local, internal) {
			matches = append(matches, r)
		}
		r.Unlock()
	}

	return matches
}
