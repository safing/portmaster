package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

// TestPutManyTimeoutAbortsBatch checks that a batch that is closed by the
// idle timeout is discarded by the storages that can abort a batch (sqlite,
// bbolt): none of its records are stored, records stored before are left
// untouched and the result says so. The hashmap cannot abort a batch and
// keeps the records passed on before the timeout.
func TestPutManyTimeoutAbortsBatch(t *testing.T) { //nolint:paralleltest,tparallel // Must finish before TestDatabaseSystem shuts down the database system.
	for _, tc := range []struct {
		storageType string
		abortable   bool
	}{
		{storageType: "sqlite", abortable: true},
		{storageType: "bbolt", abortable: true},    //nolint:goconst // Storage type name.
		{storageType: "hashmap", abortable: false}, //nolint:goconst // Storage type name.
	} {
		t.Run(tc.storageType, func(t *testing.T) {
			t.Parallel()
			testPutManyTimeoutAbortsBatch(t, tc.storageType, tc.abortable)
		})
	}
}

func testPutManyTimeoutAbortsBatch(t *testing.T, storageType string, abortable bool) {
	t.Helper()

	dbName := "test-putmany-abort-" + storageType
	_, err := Register(&Database{
		Name:        dbName,
		Description: "Unit Test Database for aborted PutMany batches",
		StorageType: storageType,
	})
	if err != nil {
		t.Fatal(err)
	}

	db := NewInterface(&Options{
		Local:    true,
		Internal: true,
	})
	batchKey := func(i int) string {
		return fmt.Sprintf("%s:batch/%d", dbName, i)
	}
	getScore := func(key string) int {
		r, err := db.Get(dbName + ":" + key)
		if err != nil {
			t.Fatalf("record %q: %s", key, err)
		}
		example, ok := r.(*Example)
		if !ok {
			example = &Example{}
			err = record.Unwrap(r, example)
			if err != nil {
				t.Fatalf("record %q: %s", key, err)
			}
		}
		return example.Score
	}
	// putBatch submits a batch that also changes a stored record. It
	// submits more records than sqlite writes with one statement.
	const records = 120
	putBatch := func(score int) (put func(record.Record) error) {
		put = db.PutMany(dbName)
		for i := range records - 1 {
			err := put(NewExample(batchKey(i), "batch", score))
			if err != nil {
				t.Fatal(err)
			}
		}
		err := put(NewExample(dbName+":changed", "batch", score))
		if err != nil {
			t.Fatal(err)
		}
		return put
	}

	// Records stored before the batch.
	for _, key := range []string{"kept", "changed"} {
		err = db.Put(NewExample(dbName+":"+key, "before", 1))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Submit a part of a batch and leave it idle until it times out.
	put := putBatch(2)
	time.Sleep(3 * time.Second)

	// Exactly one result is delivered, also when asked again.
	finished := make(chan [2]error, 1)
	go func() {
		finished <- [2]error{put(nil), put(nil)}
	}()
	var results [2]error
	select {
	case results = <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("finishing the timed out batch blocked")
	}
	err = results[0]
	if !errors.Is(err, errPutManyTimeout) {
		t.Fatalf("finishing returned %v, expected %v", err, errPutManyTimeout)
	}
	if results[1] == nil || results[1].Error() != err.Error() {
		t.Errorf("finishing again returned %v, expected %v", results[1], err)
	}

	wantStored, wantScore, wantMsg := 0, 1, "were not stored"
	if !abortable {
		wantStored, wantScore, wantMsg = records-1, 2, "were stored"
	}
	wantMsg = fmt.Sprintf("%d records submitted before %s", records, wantMsg)
	if !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("finishing returned %q, expected it to report: %s", err, wantMsg)
	}
	if errors.Is(err, storage.ErrBatchAborted) != abortable {
		t.Errorf("finishing returned %q, expected an aborted batch: %v", err, abortable)
	}

	// The batch is stored not at all, or completely by the hashmap.
	var stored int
	for i := range records - 1 {
		_, err = db.Get(batchKey(i))
		switch {
		case err == nil:
			stored++
		case !errors.Is(err, ErrNotFound):
			t.Fatal(err)
		}
	}
	if stored != wantStored {
		t.Errorf("%d new records of the timed out batch are stored, expected %d", stored, wantStored)
	}
	// Records stored before are untouched by an aborted batch.
	if score := getScore("kept"); score != 1 {
		t.Errorf("record stored before the batch has score %d, expected 1", score)
	}
	if score := getScore("changed"); score != wantScore {
		t.Errorf("record changed by the timed out batch has score %d, expected %d", score, wantScore)
	}

	// The storage is not blocked by the aborted batch and a finished batch
	// still stores everything.
	err = putBatch(3)(nil)
	if err != nil {
		t.Fatalf("finishing returned %v", err)
	}
	for i := range records - 1 {
		_, err = db.Get(batchKey(i))
		if err != nil {
			t.Fatalf("record %d of the finished batch: %s", i, err)
		}
	}
	if score := getScore("kept"); score != 1 {
		t.Errorf("record stored before the batch has score %d, expected 1", score)
	}
	if score := getScore("changed"); score != 3 {
		t.Errorf("record changed by the finished batch has score %d, expected 3", score)
	}
}

// abortObservingStorage is a test storage that implements both batch APIs
// and records which one was used and whether the batch was aborted at the
// time its channel was closed.
type abortObservingStorage struct {
	nonBatchingStorage

	plainCalls chan struct{}
	aborted    chan bool
}

func (s *abortObservingStorage) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	s.plainCalls <- struct{}{}
	return s.PutManyAbortable(context.Background(), shadowDelete)
}

func (s *abortObservingStorage) PutManyAbortable(ctx context.Context, shadowDelete bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)
	go func() {
		for range batch { //nolint:revive // Drain the batch.
		}
		// The abort must be visible as soon as the channel is closed.
		aborted := ctx.Err() != nil
		s.aborted <- aborted
		if aborted {
			errs <- fmt.Errorf("%w: %w", storage.ErrBatchAborted, context.Cause(ctx))
			return
		}
		errs <- nil
	}()
	return batch, errs
}

var putManyAbortObserver = &abortObservingStorage{
	plainCalls: make(chan struct{}, 10),
	aborted:    make(chan bool, 10),
}

func init() {
	_ = storage.Register("test-putmany-abort-observer", func(name, location string) (storage.Interface, error) {
		return putManyAbortObserver, nil
	})
}

// TestPutManyAbortSignal checks when Interface.PutMany aborts a batch of a
// storage that implements storage.AbortableBatcher: never when the caller
// finishes the batch, always when the batch times out, and always before the
// batch channel is closed.
func TestPutManyAbortSignal(t *testing.T) { //nolint:paralleltest // Must finish before TestDatabaseSystem shuts down the database system.
	dbName := "test-putmany-abort-observer-db"
	_, err := Register(&Database{
		Name:        dbName,
		Description: "Unit Test Database for the PutMany abort signal",
		StorageType: "test-putmany-abort-observer",
	})
	if err != nil {
		t.Fatal(err)
	}
	db := NewInterface(&Options{
		Local:    true,
		Internal: true,
	})
	s := putManyAbortObserver
	startBatch := func(records int) (put func(record.Record) error) {
		put = db.PutMany(dbName)
		for i := range records {
			err := put(NewExample(fmt.Sprintf("%s:%d", dbName, i), "A", i))
			if err != nil {
				t.Fatal(err)
			}
		}
		return put
	}

	// Finished batches are not aborted, with or without records.
	for _, records := range []int{0, 300} {
		err = startBatch(records)(nil)
		if err != nil {
			t.Fatalf("finishing a batch of %d records returned %v", records, err)
		}
		if <-s.aborted {
			t.Fatalf("finished batch of %d records was aborted", records)
		}
	}

	// A batch that times out is aborted, also without records.
	for _, records := range []int{0, 5} {
		put := startBatch(records)
		select {
		case aborted := <-s.aborted:
			if !aborted {
				t.Fatalf("timed out batch of %d records was not aborted before it was closed", records)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("PutMany did not time out")
		}
		err = put(nil)
		if !errors.Is(err, errPutManyTimeout) {
			t.Errorf("finishing returned %v, expected %v", err, errPutManyTimeout)
		}
		if records > 0 && !errors.Is(err, storage.ErrBatchAborted) {
			t.Errorf("finishing returned %v, expected %v", err, storage.ErrBatchAborted)
		}
	}

	if len(s.plainCalls) != 0 {
		t.Errorf("PutMany was used %d times instead of PutManyAbortable", len(s.plainCalls))
	}
}
