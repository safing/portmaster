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
func (b *BBolt) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)

	go func() {
		err := b.db.Batch(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket(bucketName)
			for r := range batch {
				txErr := b.batchPutOrDelete(bucket, shadowDelete, r)
				if txErr != nil {
					return txErr
				}
			}
			return nil
		})
		errs <- err
	}()

	return batch, errs
}

func (b *BBolt) batchPutOrDelete(bucket *bbolt.Bucket, shadowDelete bool, r record.Record) (err error) {
	r.Lock()
	defer r.Unlock()

	if !shadowDelete && r.Meta().IsDeleted() {
		// Immediate delete.
		err = bucket.Delete([]byte(r.DatabaseKey()))
	} else {
		// Put or shadow delete.
		var data []byte
		data, err = r.MarshalRecord(r)
		if err == nil {
			err = bucket.Put([]byte(r.DatabaseKey()), data)
		}
	}

	return err
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
	prefix := []byte(q.DatabaseKeyPrefix())
	err := b.db.View(func(tx *bbolt.Tx) error {
		// Create a cursor for iteration.
		c := tx.Bucket(bucketName).Cursor()

		// Iterate over items in sorted key order. This starts from the
		// first key/value pair and updates the k/v variables to the
		// next key/value on each iteration.
		//
		// The loop finishes at the end of the cursor when a nil key is returned.
		for key, value := c.Seek(prefix); key != nil; key, value = c.Next() {

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

			// check if matches & send
			if q.MatchesRecord(iterWrapper) {
				// copy data
				duplicate := make([]byte, len(value))
				copy(duplicate, value)

				newWrapper, err := record.NewRawWrapper(b.name, iterWrapper.DatabaseKey(), duplicate)
				if err != nil {
					return err
				}
				select {
				case <-queryIter.Done:
					return nil
				case queryIter.Next <- newWrapper:
				default:
					select {
					case <-queryIter.Done:
						return nil
					case queryIter.Next <- newWrapper:
					case <-time.After(1 * time.Second):
						return errors.New("query timeout")
					}
				}
			}
		}
		return nil
	})
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
func (b *BBolt) MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time, shadowDelete bool) error { //nolint:gocognit
	now := time.Now().Unix()
	purgeThreshold := purgeDeletedBefore.Unix()

	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		// Create a cursor for iteration.
		c := bucket.Cursor()
		for key, value := c.First(); key != nil; key, value = c.Next() {
			// check if context is cancelled
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			// wrap value
			wrapper, err := record.NewRawWrapper(b.name, string(key), value)
			if err != nil {
				return err
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

					continue
				}

				// Immediately delete expired entries if shadowDelete is disabled.
				fallthrough
			case meta.Deleted > 0 && (!shadowDelete || meta.Deleted < purgeThreshold):
				// delete from storage
				err = c.Delete()
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Purge deletes all records that match the given query. It returns the number of successful deletes and an error.
func (b *BBolt) Purge(ctx context.Context, q *query.Query, local, internal, shadowDelete bool) (int, error) { //nolint:gocognit
	prefix := []byte(q.DatabaseKeyPrefix())

	var cnt int
	var done bool
	for !done {
		err := b.db.Update(func(tx *bbolt.Tx) error {
			// Create a cursor for iteration.
			bucket := tx.Bucket(bucketName)
			c := bucket.Cursor()
			for key, value := c.Seek(prefix); key != nil; key, value = c.Next() {
				// Check if context has been cancelled.
				select {
				case <-ctx.Done():
					done = true
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
				cnt++
				if cnt%1000 == 0 {
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

	return cnt, nil
}

// Shutdown shuts down the database.
func (b *BBolt) Shutdown() error {
	return b.db.Close()
}
