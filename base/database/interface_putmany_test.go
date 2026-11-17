package database

import (
	"errors"
	"fmt"
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
