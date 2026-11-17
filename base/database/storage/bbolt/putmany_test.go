package bbolt

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/storage"
)

func newPutManyTestDB(t *testing.T) *BBolt {
	t.Helper()

	db, err := NewBBolt("test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Shutdown()
	})
	bb, ok := db.(*BBolt)
	if !ok {
		t.Fatal("unexpected storage type")
	}
	return bb
}

func newPutManyTestRecord(key string) *TestRecord {
	r := &TestRecord{S: key}
	r.SetKey("test:" + key)
	r.CreateMeta()
	return r
}

// newUnmarshalableRecord returns a record without meta data, which fails to
// marshal with "missing meta".
func newUnmarshalableRecord(key string) *TestRecord {
	r := &TestRecord{S: key}
	r.SetKey("test:" + key)
	return r
}

func waitForPutManyResult(t *testing.T, errs <-chan error) error {
	t.Helper()

	select {
	case err := <-errs:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("PutMany did not report a result")
		return nil
	}
}

// TestPutManyReportsErrorAtomically checks that a PutMany batch containing a
// record that cannot be stored reports the error and stores nothing.
// bbolt's DB.Batch re-runs a failing function solo, which used to continue
// with the remaining records of the channel and commit them with a nil error.
func TestPutManyReportsErrorAtomically(t *testing.T) {
	t.Parallel()

	db := newPutManyTestDB(t)

	batch, errs := db.PutMany(true)
	batch <- newPutManyTestRecord("before")
	batch <- newUnmarshalableRecord("bad")
	batch <- newPutManyTestRecord("after")
	close(batch)

	err := waitForPutManyResult(t, errs)
	if err == nil {
		t.Error("PutMany with an unmarshalable record reported success")
	}
	for _, key := range []string{"before", "bad", "after"} {
		_, err := db.Get(key)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("record %q: expected not found after failed PutMany, got %v", key, err)
		}
	}
}

// TestPutManyConcurrentFailureDoesNotLoseRecords runs a healthy and a failing
// PutMany at the same time. With DB.Batch, both were combined into one
// transaction; the failure rolled it back and the healthy function was re-run
// on its already drained channel, committing nothing while reporting success.
// Depending on the order in which bbolt combined them, either the healthy
// records were lost or the error of the failing batch was swallowed.
func TestPutManyConcurrentFailureDoesNotLoseRecords(t *testing.T) {
	t.Parallel()

	db := newPutManyTestDB(t)
	// Make bbolt combine exactly the two PutMany calls below, independent of
	// timing: the batch only starts when both calls have joined it.
	db.db.MaxBatchSize = 2
	db.db.MaxBatchDelay = time.Minute

	const healthyCount = 50

	healthy, healthyErrs := db.PutMany(true)
	failing, failingErrs := db.PutMany(true)

	for i := range healthyCount {
		healthy <- newPutManyTestRecord(fmt.Sprintf("healthy/%02d", i))
	}
	close(healthy)
	failing <- newUnmarshalableRecord("failing")
	close(failing)

	healthyErr := waitForPutManyResult(t, healthyErrs)
	failingErr := waitForPutManyResult(t, failingErrs)

	if failingErr == nil {
		t.Error("failing PutMany reported success")
	}
	if healthyErr != nil {
		t.Fatalf("healthy PutMany failed: %s", healthyErr)
	}
	var missing int
	for i := range healthyCount {
		_, err := db.Get(fmt.Sprintf("healthy/%02d", i))
		if err != nil {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("healthy PutMany reported success, but %d of %d records are missing", missing, healthyCount)
	}
}

// TestPutManyIdleProducerDoesNotBlockWriters checks that a PutMany whose
// producer is idle mid-stream does not hold the bbolt write transaction, which
// would block all other writers until the producer finishes.
func TestPutManyIdleProducerDoesNotBlockWriters(t *testing.T) {
	t.Parallel()

	db := newPutManyTestDB(t)

	batch, errs := db.PutMany(true)
	batch <- newPutManyTestRecord("streamed/1")
	// Producer idles. Give PutMany time to pick up the record (and to pass
	// bbolt's batch delay of 10ms, if it is used).
	time.Sleep(100 * time.Millisecond)

	putDone := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := db.Put(newPutManyTestRecord("other"))
		putDone <- err
	}()

	putFinished := false
	select {
	case err := <-putDone:
		putFinished = true
		if err != nil {
			t.Errorf("Put failed: %s", err)
		}
		t.Logf("Put during idle PutMany took %s", time.Since(start))
	case <-time.After(time.Second):
		t.Errorf("Put blocked for more than %s by an idle PutMany producer", time.Since(start))
	}

	// Finish the producer, which also releases a blocked Put.
	batch <- newPutManyTestRecord("streamed/2")
	close(batch)
	err := waitForPutManyResult(t, errs)
	if err != nil {
		t.Fatalf("PutMany failed: %s", err)
	}
	if !putFinished {
		select {
		case <-putDone:
			t.Logf("blocked Put finished %s after it started", time.Since(start))
		case <-time.After(10 * time.Second):
			t.Fatal("Put did not finish after PutMany finished")
		}
	}

	for _, key := range []string{"streamed/1", "streamed/2", "other"} {
		_, err := db.Get(key)
		if err != nil {
			t.Errorf("record %q: %s", key, err)
		}
	}
}
