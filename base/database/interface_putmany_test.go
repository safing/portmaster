package database

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

var errStoppedPutMany = errors.New("test storage failed to write record")

// nonBatchingStorage is a writable test storage without PutMany support.
type nonBatchingStorage struct {
	storage.InjectBase
}

func (s *nonBatchingStorage) ReadOnly() bool { return false }
func (s *nonBatchingStorage) Injected() bool { return false }

// stoppingBatchStorage is a test storage whose PutMany handler fails on the
// first record and stops reading the batch channel, like the SQLite storage
// does on a write error.
type stoppingBatchStorage struct {
	nonBatchingStorage
}

func (s *stoppingBatchStorage) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)
	go func() {
		<-batch
		errs <- errStoppedPutMany
	}()
	return batch, errs
}

func init() {
	_ = storage.Register("test-putmany-stopping", func(name, location string) (storage.Interface, error) {
		return &stoppingBatchStorage{}, nil
	})
	_ = storage.Register("test-putmany-unsupported", func(name, location string) (storage.Interface, error) {
		return &nonBatchingStorage{}, nil
	})
}

// TestPutManyStoppedBackend checks that Interface.PutMany neither blocks nor
// leaks its proxy goroutine when the storage stops reading the batch, and
// that the caller receives the storage's error.
func TestPutManyStoppedBackend(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		storageType string
		wantErr     error
	}{
		{storageType: "test-putmany-stopping", wantErr: errStoppedPutMany},
		{storageType: "test-putmany-unsupported", wantErr: ErrNotImplemented},
	} {
		t.Run(tc.storageType, func(t *testing.T) {
			t.Parallel()

			dbName := tc.storageType + "-db"
			_, err := Register(&Database{
				Name:        dbName,
				Description: "Unit Test Database for PutMany with a stopped backend",
				StorageType: tc.storageType,
			})
			if err != nil {
				t.Fatal(err)
			}

			db := NewInterface(&Options{
				Local:    true,
				Internal: true,
			})

			// putErr, finishErr, finishAgainErr
			results := make(chan [3]error, 1)
			go func() {
				var res [3]error
				put := db.PutMany(dbName)
				// Write more records than the channel buffers can hold.
				for i := range 300 {
					res[0] = put(NewExample(fmt.Sprintf("%s:%d", dbName, i), "A", i))
					if res[0] != nil {
						break
					}
				}
				// Finishing the batch must return the storage error, too,
				// also when called again.
				res[1] = put(nil)
				res[2] = put(nil)
				results <- res
			}()

			select {
			case res := <-results:
				for i, step := range []string{"put", "finish", "finish again"} {
					if !errors.Is(res[i], tc.wantErr) {
						t.Errorf("%s returned %v, expected %v", step, res[i], tc.wantErr)
					}
				}
			case <-time.After(10 * time.Second):
				t.Fatal("PutMany blocked after the storage stopped reading the batch")
			}
		})
	}
}

// TestPutManyFinish checks the result of a successful batch.
func TestPutManyFinish(t *testing.T) {
	t.Parallel()

	dbName := "test-putmany-finish"
	_, err := Register(&Database{
		Name:        dbName,
		Description: "Unit Test Database for PutMany",
		StorageType: "hashmap", //nolint:goconst // Storage type name.
	})
	if err != nil {
		t.Fatal(err)
	}

	db := NewInterface(&Options{
		Local:    true,
		Internal: true,
	})

	put := db.PutMany(dbName)
	for i := range 300 {
		err = put(NewExample(fmt.Sprintf("%s:%d", dbName, i), "A", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	err = put(nil)
	if err != nil {
		t.Fatalf("finishing returned %v", err)
	}
	err = put(nil)
	if err != nil {
		t.Fatalf("finishing again returned %v", err)
	}
	err = put(NewExample(dbName+":late", "A", 1))
	if !errors.Is(err, errBatchClosed) {
		t.Fatalf("put after finishing returned %v, expected %v", err, errBatchClosed)
	}

	for i := range 300 {
		_, err = db.Get(fmt.Sprintf("%s:%d", dbName, i))
		if err != nil {
			t.Fatal(err)
		}
	}
}

var errCommitPutMany = errors.New("test storage failed to commit batch")

// committingBatchStorage is a test storage that, like the SQLite and BBolt
// storages, collects the batch until the batch channel is closed and then
// commits it. The commit only completes once release is closed.
type committingBatchStorage struct {
	nonBatchingStorage

	commitErr error
	closed    chan struct{} // Closed when the batch channel was closed.
	release   chan struct{} // Close to let the commit complete.
	committed chan int      // Receives the number of committed records.
}

func newCommittingBatchStorage(commitErr error) *committingBatchStorage {
	return &committingBatchStorage{
		commitErr: commitErr,
		closed:    make(chan struct{}),
		release:   make(chan struct{}),
		committed: make(chan int, 1),
	}
}

func (s *committingBatchStorage) PutMany(shadowDelete bool) (chan<- record.Record, <-chan error) {
	batch := make(chan record.Record, 100)
	errs := make(chan error, 1)
	go func() {
		var n int
		for range batch {
			n++
		}
		close(s.closed)
		<-s.release
		if s.commitErr == nil {
			s.committed <- n
		}
		errs <- s.commitErr
	}()
	return batch, errs
}

var (
	putManyCommitOK     = newCommittingBatchStorage(nil)
	putManyCommitFailed = newCommittingBatchStorage(errCommitPutMany)
)

func init() {
	_ = storage.Register("test-putmany-commit-ok", func(name, location string) (storage.Interface, error) {
		return putManyCommitOK, nil
	})
	_ = storage.Register("test-putmany-commit-failed", func(name, location string) (storage.Interface, error) {
		return putManyCommitFailed, nil
	})
}

// TestPutManyTimeoutReportsCommit checks what the caller learns when the
// PutMany batch times out: the storage commits the records that were passed
// on before the timeout, as the batch channel is closed normally. The caller
// must only get the final result once the storage finished the batch, and the
// result must say whether these records were stored.
func TestPutManyTimeoutReportsCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		storageType string
		storage     *committingBatchStorage
	}{
		{storageType: "test-putmany-commit-ok", storage: putManyCommitOK},
		{storageType: "test-putmany-commit-failed", storage: putManyCommitFailed},
	} {
		t.Run(tc.storageType, func(t *testing.T) {
			t.Parallel()

			dbName := tc.storageType + "-db"
			_, err := Register(&Database{
				Name:        dbName,
				Description: "Unit Test Database for PutMany timeouts",
				StorageType: tc.storageType,
			})
			if err != nil {
				t.Fatal(err)
			}

			db := NewInterface(&Options{
				Local:    true,
				Internal: true,
			})

			const records = 5
			put := db.PutMany(dbName)
			for i := range records {
				err = put(NewExample(fmt.Sprintf("%s:%d", dbName, i), "A", i))
				if err != nil {
					t.Fatal(err)
				}
			}

			// Wait for the idle timeout to close the batch.
			select {
			case <-tc.storage.closed:
			case <-time.After(10 * time.Second):
				t.Fatal("PutMany did not time out")
			}

			finished := make(chan error, 1)
			go func() {
				finished <- put(nil)
			}()

			// The result is not known before the storage finished the batch.
			select {
			case err = <-finished:
				t.Fatalf("finishing returned %v before the storage finished the batch", err)
			case <-time.After(200 * time.Millisecond):
			}
			close(tc.storage.release)

			select {
			case err = <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("finishing the batch blocked")
			}
			if !errors.Is(err, errPutManyTimeout) {
				t.Errorf("finishing returned %v, expected %v", err, errPutManyTimeout)
			}
			if tc.storage.commitErr != nil {
				if !errors.Is(err, tc.storage.commitErr) {
					t.Errorf("finishing returned %v, expected it to include %v", err, tc.storage.commitErr)
				}
				return
			}

			if n := <-tc.storage.committed; n != records {
				t.Fatalf("storage committed %d records, expected %d", n, records)
			}
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d records submitted before were stored", records)) {
				t.Errorf("finishing returned %v, expected it to report that %d records were stored", err, records)
			}
			// Further puts are rejected with the same result.
			againErr := put(NewExample(dbName+":late", "A", 1))
			if againErr == nil || againErr.Error() != err.Error() {
				t.Errorf("put after the timeout returned %v, expected %v", againErr, err)
			}
		})
	}
}
