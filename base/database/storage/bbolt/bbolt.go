package bbolt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

var bucketName = []byte{0}

// queryPageSize is the maximum number of matching records a query collects
// within one read transaction before handing them to the consumer.
const queryPageSize = 100

var errQueryTimeout = errors.New("query timeout")

// BBolt database made pluggable for portbase.
type BBolt struct {
	name string
	db   *bbolt.DB
}

func init() {
	_ = storage.Register("bbolt", NewBBolt)
}

// NewBBolt opens/creates a bbolt database.
func NewBBolt(name, location string) (storage.Interface, error) {
	// Create options for bbolt database.
	dbFile := filepath.Join(location, "db.bbolt")
	dbOptions := &bbolt.Options{
		Timeout: 1 * time.Second,
	}

	// Open/Create database, retry if there is a timeout.
	db, err := bbolt.Open(dbFile, 0o0600, dbOptions)
	for i := 0; i < 5 && err != nil; i++ {
		// Try again if there is an error.
		db, err = bbolt.Open(dbFile, 0o0600, dbOptions)
	}
	if err != nil {
		return nil, err
	}

	// Create bucket
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// Close the database in order to release its file lock, which would
		// otherwise block every later attempt to open it.
		_ = db.Close()
		return nil, err
	}

	return &BBolt{
		name: name,
		db:   db,
	}, nil
}

// Get returns a database record.
func (b *BBolt) Get(key string) (record.Record, error) {
	var r record.Record

	err := b.db.View(func(tx *bbolt.Tx) error {
		// get value from db
		value := tx.Bucket(bucketName).Get([]byte(key))
		if value == nil {
			return storage.ErrNotFound
		}

		// copy data
		duplicate := make([]byte, len(value))
		copy(duplicate, value)

		// create record
		var txErr error
		r, txErr = record.NewRawWrapper(b.name, key, duplicate)
		if txErr != nil {
			return txErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetMeta returns the metadata of a database record.
func (b *BBolt) GetMeta(key string) (*record.Meta, error) {
	// TODO: Replace with more performant variant.

	r, err := b.Get(key)
	if err != nil {
		return nil, err
	}

	return r.Meta(), nil
}

// Put stores a record in the database.
func (b *BBolt) Put(r record.Record) (record.Record, error) {
	data, err := r.MarshalRecord(r)
	if err != nil {
		return nil, err
	}

	err = b.db.Update(func(tx *bbolt.Tx) error {
		txErr := tx.Bucket(bucketName).Put([]byte(r.DatabaseKey()), data)
		if txErr != nil {
			return txErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// PutMany stores many records in the database.
// The records are received and marshaled outside of any transaction and are
// written in a single transaction once the batch channel is closed, so that
// either all or none of them are stored and a slow producer does not block
// other writers. Exactly one value is sent on the error channel.
func (b *BBolt) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	return b.PutManyAbortable(context.Background(), shadowDelete)
}

// PutManyAbortable stores many records in the database like PutMany, unless
// abort is done before the batch channel is closed: the collected records are
// then discarded without opening a transaction and the result wraps
// storage.ErrBatchAborted.
func (b *BBolt) PutManyAbortable(abort context.Context, shadowDelete bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	go func() {
		errs <- b.putMany(abort, batch, shadowDelete)
	}()

	return batch, errs
}

// Delete deletes a record from the database.
func (b *BBolt) Delete(key string) error {
	err := b.db.Update(func(tx *bbolt.Tx) error {
		txErr := tx.Bucket(bucketName).Delete([]byte(key))
		if txErr != nil {
			return txErr
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// Query returns a an iterator for the supplied query.
func (b *BBolt) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	_, err := q.Check()
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	queryIter := iterator.New()

	go b.queryExecutor(queryIter, q, local, internal)
	return queryIter, nil
}

func (b *BBolt) queryExecutor(queryIter *iterator.Iterator, q *query.Query, local, internal bool) {
	// Collect the matching records page by page within a read transaction,
	// but feed them to the consumer only after the transaction is closed.
	// An open read transaction blocks bbolt from growing its mmap, so a slow
	// consumer would otherwise stall the writer that needs to grow it and,
	// through it, all other writers and new transactions.
	var (
		err      error
		startKey = []byte(q.DatabaseKeyPrefix())
	)
	for {
		var page []record.Record
		page, startKey, err = b.queryPage(q, startKey, local, internal)

		// Feed the records collected before a possible error, as before.
		done, feedErr := feedQueryPage(queryIter, page)
		if done {
			err = feedErr
			break
		}
		if err != nil || startKey == nil {
			break
		}
	}
	queryIter.Finish(err)
}

// ReadOnly returns whether the database is read only.
func (b *BBolt) ReadOnly() bool {
	return false
}

// Injected returns whether the database is injected.
func (b *BBolt) Injected() bool {
	return false
}

// MaintainRecordStates maintains records states in the database.
// The records are processed in chunks of maintenanceChunkSize records, each
// in its own write transaction, so that other writers are not blocked for the
// whole walk. If the context is cancelled, the already processed chunks stay
// committed and the context error is returned.
func (b *BBolt) MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time, shadowDelete bool) error {
	now := time.Now().Unix()
	purgeThreshold := purgeDeletedBefore.Unix()

	var startKey []byte
	for {
		// Check if the context is cancelled.
		err := ctx.Err()
		if err != nil {
			return err
		}

		err = b.db.Update(func(tx *bbolt.Tx) error {
			var txErr error
			startKey, txErr = b.maintainRecordStatesChunk(tx.Bucket(bucketName), startKey, now, purgeThreshold, shadowDelete)
			return txErr
		})
		if err != nil && !errors.Is(err, errNothingToMaintain) {
			return err
		}
		if startKey == nil {
			return nil
		}
	}
}

// Purge deletes all records that match the given query. It returns the number of successful deletes and an error.
func (b *BBolt) Purge(ctx context.Context, q *query.Query, local, internal, shadowDelete bool) (int, error) { //nolint:gocognit
	prefix := []byte(q.DatabaseKeyPrefix())

	var cnt int
	var done bool
	var cancelErr error
	// Each chunk continues after the last record handled by the previous one,
	// instead of rescanning the records already handled, which stay in the
	// bucket when shadow deleting or skipped.
	startKey := prefix
	for !done {
		err := b.db.Update(func(tx *bbolt.Tx) error {
			// Create a cursor for iteration.
			bucket := tx.Bucket(bucketName)
			c := bucket.Cursor()
			for key, value := c.Seek(startKey); key != nil; key, value = c.Next() {
				// Check if context has been cancelled.
				select {
				case <-ctx.Done():
					// Commit what was purged so far, but report that the
					// purge is incomplete.
					done = true
					cancelErr = ctx.Err()
					return nil
				default:
				}

				// Check if we still match the key prefix, if not, exit.
				if !bytes.HasPrefix(key, prefix) {
					done = true
					return nil
				}

				// Wrap the value in a new wrapper to access the metadata.
				wrapper, err := record.NewRawWrapper(b.name, string(key), value)
				if err != nil {
					return err
				}

				// Check if we have permission for this record.
				if !wrapper.Meta().CheckPermission(local, internal) {
					continue
				}

				// Check if record is already deleted.
				if wrapper.Meta().IsDeleted() {
					continue
				}

				// Check if the query matches this record.
				if !q.MatchesRecord(wrapper) {
					continue
				}

				// Delete record.
				if shadowDelete {
					// Shadow delete.
					wrapper.Meta().Delete()
					deleted, err := wrapper.MarshalRecord(wrapper)
					if err != nil {
						return err
					}
					err = bucket.Put(key, deleted)
					if err != nil {
						return err
					}

					// Cursor repositioning is required after modifying data.
					// While the documentation states that this is also required after a
					// delete, this actually makes the cursor skip a record with the
					// following c.Next() call of the loop.
					// Docs/Issue: https://github.com/boltdb/bolt/issues/426#issuecomment-141982984
					c.Seek(key)

				} else {
					// Immediate delete.
					err = c.Delete()
					if err != nil {
						return err
					}
				}

				// Work in batches of 1000 changes in order to enable other operations in between.
				// The next chunk starts at the smallest key after the current one.
				cnt++
				if cnt%1000 == 0 {
					startKey = make([]byte, len(key)+1)
					copy(startKey, key)
					return nil
				}
			}
			done = true
			return nil
		})
		if err != nil {
			return cnt, err
		}
	}

	return cnt, cancelErr
}

// Shutdown shuts down the database.
func (b *BBolt) Shutdown() error {
	return b.db.Close()
}

// maintenanceChunkSize is the maximum number of records MaintainRecordStates
// processes within one write transaction. Each commit costs a sync to disk, so
// the chunks must not be too small. A variable for tests.
var maintenanceChunkSize = 10000

// errNothingToMaintain rolls back the transaction of a chunk without changes,
// which saves the commit and its sync to disk.
var errNothingToMaintain = errors.New("nothing to maintain")

// maintenanceChange is a change of a record found during maintenance.
type maintenanceChange struct {
	key  []byte
	data []byte // nil means delete
}

// maintainRecordStatesChunk maintains up to maintenanceChunkSize records,
// starting at the given key. It returns the key to continue at, or nil if
// there are no more records. If no record needs to be changed, it returns
// errNothingToMaintain, together with the key to continue at.
func (b *BBolt) maintainRecordStatesChunk(bucket *bbolt.Bucket, startKey []byte, now, purgeThreshold int64, shadowDelete bool) (nextKey []byte, err error) {
	// Collect the changes first and apply them after the iteration, as
	// modifying the bucket during cursor iteration (other than with a lone
	// Cursor.Delete) makes the cursor skip records.
	var changes []maintenanceChange
	c := bucket.Cursor()
	processed := 0
	for key, value := c.Seek(startKey); key != nil; key, value = c.Next() {
		// Continue in a new transaction once the chunk is full.
		if processed >= maintenanceChunkSize {
			nextKey = bytes.Clone(key)
			break
		}
		processed++

		// wrap value
		wrapper, err := record.NewRawWrapper(b.name, string(key), value)
		if err != nil {
			return nil, err
		}

		// check if we need to do maintenance
		meta := wrapper.Meta()
		switch {
		case meta.Deleted == 0 && meta.Expires > 0 && meta.Expires < now:
			if shadowDelete {
				// mark as deleted
				meta.Deleted = meta.Expires
				deleted, err := wrapper.MarshalRecord(wrapper)
				if err != nil {
					return nil, err
				}
				changes = append(changes, maintenanceChange{key: bytes.Clone(key), data: deleted})
				continue
			}

			// Immediately delete expired entries if shadowDelete is disabled.
			fallthrough
		case meta.Deleted > 0 && (!shadowDelete || meta.Deleted < purgeThreshold):
			// delete from storage
			changes = append(changes, maintenanceChange{key: bytes.Clone(key)})
		}
	}

	if len(changes) == 0 {
		return nextKey, errNothingToMaintain
	}

	// Apply the changes.
	for _, change := range changes {
		if change.data == nil {
			err = bucket.Delete(change.key)
		} else {
			err = bucket.Put(change.key, change.data)
		}
		if err != nil {
			return nil, err
		}
	}

	return nextKey, nil
}

// queryPage collects up to queryPageSize matching records, starting at the
// given key, within one read transaction. It returns the key to continue at,
// or nil if there are no more records. The returned records own their data,
// as the data returned by bbolt is only valid during the transaction.
func (b *BBolt) queryPage(q *query.Query, startKey []byte, local, internal bool) (page []record.Record, nextKey []byte, err error) {
	prefix := []byte(q.DatabaseKeyPrefix())
	err = b.db.View(func(tx *bbolt.Tx) error {
		// Create a cursor for iteration.
		c := tx.Bucket(bucketName).Cursor()

		// Iterate over items in sorted key order. This starts from the
		// first key/value pair and updates the k/v variables to the
		// next key/value on each iteration.
		//
		// The loop finishes at the end of the cursor when a nil key is returned.
		for key, value := c.Seek(startKey); key != nil; key, value = c.Next() {
			// if we don't match the prefix anymore, exit
			if !bytes.HasPrefix(key, prefix) {
				return nil
			}

			// wrap value
			iterWrapper, err := record.NewRawWrapper(b.name, string(key), value)
			if err != nil {
				return err
			}

			// check validity / access
			if !iterWrapper.Meta().CheckValidity() {
				continue
			}
			if !iterWrapper.Meta().CheckPermission(local, internal) {
				continue
			}

			// check if matches & collect
			if !q.MatchesRecord(iterWrapper) {
				continue
			}

			// copy data
			duplicate := make([]byte, len(value))
			copy(duplicate, value)

			newWrapper, err := record.NewRawWrapper(b.name, iterWrapper.DatabaseKey(), duplicate)
			if err != nil {
				return err
			}
			page = append(page, newWrapper)

			// Continue in a new transaction once the page is full. The next
			// page starts at the smallest key after the current one.
			if len(page) >= queryPageSize {
				nextKey = make([]byte, len(key)+1)
				copy(nextKey, key)
				return nil
			}
		}
		return nil
	})
	return page, nextKey, err
}

// feedQueryPage sends the given records to the query iterator. It returns
// whether the query is done, either because it was cancelled or because the
// consumer did not accept a record in time.
func feedQueryPage(queryIter *iterator.Iterator, page []record.Record) (done bool, err error) {
	for _, r := range page {
		select {
		case <-queryIter.Done:
			return true, nil
		case queryIter.Next <- r:
		default:
			select {
			case <-queryIter.Done:
				return true, nil
			case queryIter.Next <- r:
			case <-time.After(1 * time.Second):
				return true, errQueryTimeout
			}
		}
	}

	return false, nil
}

// batchEntry is a prepared write of a PutMany batch.
type batchEntry struct {
	key    []byte
	data   []byte
	delete bool
}

// putMany collects all records of the batch and writes them in one
// transaction. It must not use DB.Batch: bbolt may run a Batch function more
// than once (when it is combined with a failing one, or when it fails itself),
// and the records of the channel can only be consumed once.
// If abort is done before the batch channel is closed, nothing is written.
func (b *BBolt) putMany(abort context.Context, batch <-chan record.Record, shadowDelete bool) error {
	var entries []batchEntry
collect:
	for {
		select {
		case r, ok := <-batch:
			if !ok {
				break collect
			}
			entry, err := prepareBatchEntry(r, shadowDelete)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		case <-abort.Done():
			break collect
		}
	}
	// Also check for an abort when the batch channel was closed, as it is
	// closed right after the abort.
	if abort.Err() != nil {
		return fmt.Errorf("%w: %w", storage.ErrBatchAborted, context.Cause(abort))
	}
	if len(entries) == 0 {
		return nil
	}

	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		for _, entry := range entries {
			var err error
			if entry.delete {
				err = bucket.Delete(entry.key)
			} else {
				err = bucket.Put(entry.key, entry.data)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func prepareBatchEntry(r record.Record, shadowDelete bool) (batchEntry, error) {
	r.Lock()
	defer r.Unlock()

	key := []byte(r.DatabaseKey())
	if !shadowDelete && r.Meta() != nil && r.Meta().IsDeleted() {
		// Immediate delete.
		return batchEntry{key: key, delete: true}, nil
	}

	// Put or shadow delete.
	data, err := r.MarshalRecord(r)
	if err != nil {
		return batchEntry{}, err
	}
	return batchEntry{key: key, data: data}, nil
}
