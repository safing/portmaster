package bbolt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

var errTestAbort = errors.New("test aborts the batch")

const abortBatchSize = 120

// feedAbortTestBatch feeds a batch that adds abortBatchSize records and
// changes the stored record "changed".
func feedAbortTestBatch(batch chan<- record.Record, value string) {
	for i := range abortBatchSize {
		batch <- newPutManyTestRecord(fmt.Sprintf("batch/%d", i))
	}
	r := newPutManyTestRecord("changed")
	r.S = value
	batch <- r
}

// checkAbortTestState checks the records "kept" and "changed", which are
// stored before the batch, and how many records the batch added.
func checkAbortTestState(t *testing.T, db *BBolt, changedValue string, wantAdded int) {
	t.Helper()

	for key, want := range map[string]string{"kept": "old", "changed": changedValue} {
		r, err := db.Get(key)
		if err != nil {
			t.Fatalf("record %q stored before the batch: %s", key, err)
		}
		got := &TestRecord{}
		err = record.Unwrap(r, got)
		if err != nil {
			t.Fatal(err)
		}
		if got.S != want {
			t.Errorf("record %q stored before the batch has value %q, expected %q", key, got.S, want)
		}
	}

	var added int
	for i := range abortBatchSize {
		_, err := db.Get(fmt.Sprintf("batch/%d", i))
		switch {
		case err == nil:
			added++
		case !errors.Is(err, storage.ErrNotFound):
			t.Fatal(err)
		}
	}
	if added != wantAdded {
		t.Errorf("the batch added %d records, expected %d", added, wantAdded)
	}
}

// checkSinglePutManyResult checks that no further result is sent and that
// the batch handler has finished.
func checkSinglePutManyResult(t *testing.T, errs <-chan error) {
	t.Helper()

	select {
	case err := <-errs:
		t.Fatalf("received second result: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	buf := make([]byte, 1<<20)
	for range 500 {
		n := runtime.Stack(buf, true)
		if !bytes.Contains(buf[:n], []byte("bbolt.(*BBolt).putMany")) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("batch handler is still running")
}

// TestPutManyAbort checks that an aborted batch is discarded: none of its
// records are stored, records stored before are untouched, exactly one
// result that is not a success is sent and the batch handler ends.
// A batch that is not aborted is still stored completely.
func TestPutManyAbort(t *testing.T) { //nolint:paralleltest // Checks for batch handlers of this test, of which other tests have their own.
	var _ storage.AbortableBatcher = &BBolt{}

	for _, closeBatch := range []bool{true, false} { //nolint:paralleltest // See above.
		t.Run(fmt.Sprintf("close=%v", closeBatch), func(t *testing.T) {
			db := newPutManyTestDB(t)
			for _, key := range []string{"kept", "changed"} {
				r := newPutManyTestRecord(key)
				r.S = "old"
				_, err := db.Put(r)
				if err != nil {
					t.Fatal(err)
				}
			}

			// Abort a batch after feeding it. The batch channel is closed
			// right after the abort, as Interface.PutMany does it, or is
			// left open.
			abort, cancel := context.WithCancelCause(context.Background())
			batch, errs := db.PutManyAbortable(abort, true)
			feedAbortTestBatch(batch, "aborted")
			cancel(errTestAbort)
			if closeBatch {
				close(batch)
			}
			err := waitForPutManyResult(t, errs)
			if !errors.Is(err, storage.ErrBatchAborted) || !errors.Is(err, errTestAbort) {
				t.Fatalf("aborted batch returned %v, expected %v with cause %v", err, storage.ErrBatchAborted, errTestAbort)
			}
			checkSinglePutManyResult(t, errs)
			checkAbortTestState(t, db, "old", 0)

			// A batch that is not aborted is stored. Cancelling after the
			// result was sent has no effect.
			abort, cancel = context.WithCancelCause(context.Background())
			batch, errs = db.PutManyAbortable(abort, true)
			feedAbortTestBatch(batch, "finished")
			close(batch)
			err = waitForPutManyResult(t, errs)
			if err != nil {
				t.Fatalf("finished batch returned %v", err)
			}
			cancel(errTestAbort)
			checkSinglePutManyResult(t, errs)
			checkAbortTestState(t, db, "finished", abortBatchSize)
		})
	}
}

// TestPutManyAbortBeforeClose checks that a batch is always aborted when the
// abort is signalled before the batch channel is closed, also if the handler
// only gets to look at them afterwards.
func TestPutManyAbortBeforeClose(t *testing.T) {
	t.Parallel()

	db := newPutManyTestDB(t)

	// Repeat, as the handler chooses randomly between a closed batch channel
	// and the abort signal when it sees both at once.
	for i := range 200 {
		abort, cancel := context.WithCancelCause(context.Background())
		batch, errs := db.PutManyAbortable(abort, true)
		batch <- newPutManyTestRecord("aborted")
		cancel(errTestAbort)
		close(batch)
		err := waitForPutManyResult(t, errs)
		if !errors.Is(err, storage.ErrBatchAborted) {
			t.Fatalf("aborted batch %d returned %v, expected %v", i, err, storage.ErrBatchAborted)
		}
	}
	_, err := db.Get("aborted")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("aborted batches must not be stored, got %v", err)
	}
}
