package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/database/storage/hashmap"
	"github.com/safing/portmaster/service/mgr"
)

// abortingStorage is a test storage that implements storage.AbortableBatcher
// on top of a hashmap. It discards the next abortBatches batches that contain
// records and reports them as aborted, like sqlite and bbolt do with a batch
// that was aborted. Other batches are stored completely.
type abortingStorage struct {
	storage.Interface

	abortBatches atomic.Int32
	aborted      atomic.Int32
}

func (s *abortingStorage) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	return s.PutManyAbortable(context.Background(), shadowDelete)
}

func (s *abortingStorage) PutManyAbortable(_ context.Context, _ bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)
	go func() {
		var records []record.Record
		for r := range batch {
			records = append(records, r)
		}
		if len(records) > 0 && s.abortBatches.Add(-1) >= 0 {
			s.aborted.Add(1)
			errs <- fmt.Errorf("%w: test abort of %d records", storage.ErrBatchAborted, len(records))
			return
		}
		for _, r := range records {
			_, err := s.Put(r)
			if err != nil {
				errs <- err
				return
			}
		}
		errs <- nil
	}()
	return batch, errs
}

var (
	abortingStoragesLock sync.Mutex
	abortingStorages     = make(map[string]*abortingStorage)
)

func init() {
	_ = storage.Register("test-aborting", func(name, location string) (storage.Interface, error) {
		hm, err := hashmap.NewHashMap(name, location)
		if err != nil {
			return nil, err
		}
		s := &abortingStorage{Interface: hm}
		abortingStoragesLock.Lock()
		defer abortingStoragesLock.Unlock()
		abortingStorages[name] = s
		return s, nil
	})
}

// setupAbortingCacheDB registers a database on an abortingStorage and returns
// it together with an interface that delays writes to it and one without a
// cache to check the storage.
func setupAbortingCacheDB(t *testing.T, dbName string) (s *abortingStorage, cachingDB, plainDB *Interface) {
	t.Helper()

	_, err := Register(&Database{
		Name:        dbName,
		Description: "Unit Test Database for aborted write cache flushes",
		StorageType: "test-aborting",
	})
	if err != nil {
		t.Fatal(err)
	}
	cachingDB = NewInterface(&Options{
		Local:             true,
		Internal:          true,
		CacheSize:         1000,
		DelayCachedWrites: dbName,
	})
	plainDB = NewInterface(&Options{
		Local:    true,
		Internal: true,
	})
	// Get the controller so that the storage is started.
	_, err = getController(dbName)
	if err != nil {
		t.Fatal(err)
	}
	abortingStoragesLock.Lock()
	s = abortingStorages[dbName]
	abortingStoragesLock.Unlock()
	if s == nil {
		t.Fatal("aborting storage was not created")
	}
	return s, cachingDB, plainDB
}

// countStored returns how many of the records 0 to n-1 are stored with the
// given score.
func countStored(t *testing.T, plainDB *Interface, dbName string, n, score int) int {
	t.Helper()

	var stored int
	for i := range n {
		r, err := plainDB.Get(fmt.Sprintf("%s:%d", dbName, i))
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			t.Fatal(err)
		}
		example, ok := r.(*Example)
		if !ok {
			example = &Example{}
			err = record.Unwrap(r, example)
			if err != nil {
				t.Fatal(err)
			}
		}
		if example.Score == score {
			stored++
		}
	}
	return stored
}

func pendingWrites(i *Interface) int {
	i.writeCacheLock.Lock()
	defer i.writeCacheLock.Unlock()
	return len(i.writeCache)
}

// TestFlushWriteCacheKeepsAbortedRecords checks that a write cache flush
// whose batch is aborted keeps the records in the write cache and that the
// next flush stores all of them, in their latest version.
func TestFlushWriteCacheKeepsAbortedRecords(t *testing.T) { //nolint:paralleltest // Must finish before TestDatabaseSystem shuts down the database system.
	dbName := "test-flush-write-cache-abort"
	s, cachingDB, plainDB := setupAbortingCacheDB(t, dbName)

	const records = 50
	for i := range records {
		err := cachingDB.Put(NewExample(fmt.Sprintf("%s:%d", dbName, i), "A", 1))
		if err != nil {
			t.Fatal(err)
		}
	}

	// The batch of the first flush is aborted: nothing is stored and all
	// records stay in the write cache.
	s.abortBatches.Store(1)
	cachingDB.FlushCache()
	if aborted := s.aborted.Load(); aborted != 1 {
		t.Fatalf("%d batches were aborted, expected 1", aborted)
	}
	stored := countStored(t, plainDB, dbName, records, 1)
	if stored != 0 {
		t.Errorf("%d records of the aborted flush are stored, expected 0", stored)
	}
	if pending := pendingWrites(cachingDB); pending != records {
		t.Fatalf("%d records are left in the write cache after the aborted flush, expected %d", pending, records)
	}

	// A newer version written in between replaces the kept one.
	err := cachingDB.Put(NewExample(dbName+":0", "A", 2))
	if err != nil {
		t.Fatal(err)
	}

	// The next flush stores all records.
	cachingDB.FlushCache()
	stored = countStored(t, plainDB, dbName, records, 1)
	if stored != records-1 {
		t.Errorf("%d records are stored after the retry, expected %d", stored, records-1)
	}
	stored = countStored(t, plainDB, dbName, 1, 2)
	if stored != 1 {
		t.Error("the newer version of record 0 is not stored after the retry")
	}
	if pending := pendingWrites(cachingDB); pending != 0 {
		t.Errorf("%d records are left in the write cache after the retry, expected 0", pending)
	}
}

// TestDelayedCacheWriterRetriesAbortedFlushOnShutdown checks that the final
// flush on shutdown is retried once if its batch is aborted.
func TestDelayedCacheWriterRetriesAbortedFlushOnShutdown(t *testing.T) { //nolint:paralleltest // Must finish before TestDatabaseSystem shuts down the database system.
	dbName := "test-delayed-cache-writer-abort"
	s, cachingDB, plainDB := setupAbortingCacheDB(t, dbName)

	m := mgr.New("delayed cache writer abort test")
	writerErr := make(chan error, 1)
	m.Go("delayed cache writer", func(wc *mgr.WorkerCtx) error {
		writerErr <- cachingDB.DelayedCacheWriter(wc)
		return nil
	})

	// Few records compared to the cache size, so that only the shutdown
	// flushes them.
	const records = 20
	for i := range records {
		err := cachingDB.Put(NewExample(fmt.Sprintf("%s:%d", dbName, i), "A", 1))
		if err != nil {
			t.Fatal(err)
		}
	}

	s.abortBatches.Store(1)
	m.Cancel()
	select {
	case err := <-writerErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("delayed cache writer did not stop")
	}

	if aborted := s.aborted.Load(); aborted != 1 {
		t.Errorf("%d batches were aborted, expected 1", aborted)
	}
	stored := countStored(t, plainDB, dbName, records, 1)
	if stored != records {
		t.Errorf("%d records are stored after shutdown, expected %d", stored, records)
	}
	if pending := pendingWrites(cachingDB); pending != 0 {
		t.Errorf("%d records are left in the write cache after shutdown, expected 0", pending)
	}
}
