package sqlite

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

var errTestAbort = errors.New("test aborts the batch")

// abortBatchSize is more than one statement of the prepared variant writes,
// so that an aborted batch has already written records in its transaction.
const abortBatchSize = 2*upsertRowsPerStatement + 20

// feedAbortTestBatch feeds a batch that adds abortBatchSize records and
// changes the stored record "changed".
func feedAbortTestBatch(batch chan<- record.Record, value string) {
	for i := range abortBatchSize {
		r := newPutManyTestRecord("batch/" + strconv.Itoa(i))
		r.S = value
		batch <- r
	}
	r := newPutManyTestRecord("changed")
	r.S = value
	batch <- r
}

// requireAbortTestState checks the records "kept" and "changed", which are
// stored before the batch, and how many records the batch added.
func requireAbortTestState(t *testing.T, db *SQLite, changedValue string, wantAdded int) {
	t.Helper()

	for key, want := range map[string]string{"kept": "before", "changed": changedValue} {
		r, err := db.Get(key)
		require.NoError(t, err, "record %q stored before the batch", key)
		got := &TestRecord{}
		require.NoError(t, record.Unwrap(r, got))
		assert.Equal(t, want, got.S, "record %q stored before the batch", key)
	}

	var added int
	for i := range abortBatchSize {
		_, err := db.Get("batch/" + strconv.Itoa(i))
		if err == nil {
			added++
		} else {
			require.ErrorIs(t, err, storage.ErrNotFound)
		}
	}
	assert.Equal(t, wantAdded, added, "records added by the batch")
}

// requireNoSecondPutManyResult checks that no further result is sent.
func requireNoSecondPutManyResult(t *testing.T, errs <-chan error) {
	t.Helper()

	select {
	case err := <-errs:
		t.Fatalf("received second result: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestPutManyAbort checks that an aborted batch is rolled back: none of its
// records are stored, records stored before are untouched, exactly one
// result that is not a success is sent and the database is released.
// A batch that is not aborted is still committed completely.
func TestPutManyAbort(t *testing.T) { //nolint:paralleltest // Switches package-wide setting.
	runPutManyVariants(t, testPutManyAbort)
}

func testPutManyAbort(t *testing.T) {
	t.Helper()

	var _ storage.AbortableBatcher = &SQLite{}

	for _, closeBatch := range []bool{true, false} {
		t.Run("close="+strconv.FormatBool(closeBatch), func(t *testing.T) {
			db, err := openSQLite("test", t.TempDir(), false)
			require.NoError(t, err)
			for _, key := range []string{"kept", "changed"} {
				r := newPutManyTestRecord(key)
				r.S = "before"
				_, err = db.Put(r)
				require.NoError(t, err)
			}

			// Abort a batch after feeding it. The batch channel is closed
			// right after the abort, as Interface.PutMany does it, or is
			// left open.
			abort, cancel := context.WithCancelCause(context.Background())
			batch, errs := db.PutManyAbortable(abort, false)
			feedAbortTestBatch(batch, "aborted")
			cancel(errTestAbort)
			if closeBatch {
				close(batch)
			}
			err = readPutManyResult(t, errs)
			require.ErrorIs(t, err, storage.ErrBatchAborted)
			require.ErrorIs(t, err, errTestAbort)
			requireNoSecondPutManyResult(t, errs)

			// The transaction is rolled back, which also releases the
			// (single) database connection for the checks.
			requireAbortTestState(t, db, "before", 0)

			// A batch that is not aborted is committed. Cancelling after
			// the result was sent has no effect.
			abort, cancel = context.WithCancelCause(context.Background())
			batch, errs = db.PutManyAbortable(abort, false)
			feedAbortTestBatch(batch, "finished")
			close(batch)
			require.NoError(t, readPutManyResult(t, errs))
			cancel(errTestAbort)
			requireNoSecondPutManyResult(t, errs)
			requireAbortTestState(t, db, "finished", abortBatchSize)

			// Shutdown waits for the batch handlers, so none is left behind.
			shutdownDone := make(chan error, 1)
			go func() {
				shutdownDone <- db.Shutdown()
			}()
			select {
			case err := <-shutdownDone:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("a batch handler is still running")
			}
		})
	}
}

// TestPutManyAbortBeforeClose checks that a batch is always aborted when the
// abort is signalled before the batch channel is closed, also if the handler
// only gets to look at them afterwards.
func TestPutManyAbortBeforeClose(t *testing.T) { //nolint:paralleltest // Switches package-wide setting.
	runPutManyVariants(t, testPutManyAbortBeforeClose)
}

func testPutManyAbortBeforeClose(t *testing.T) {
	t.Helper()

	db, err := openSQLite("test", t.TempDir(), false)
	require.NoError(t, err)

	// Repeat, as the handler chooses randomly between a closed batch channel
	// and the abort signal when it sees both at once.
	for i := range 200 {
		abort, cancel := context.WithCancelCause(context.Background())
		batch, errs := db.PutManyAbortable(abort, false)
		batch <- newPutManyTestRecord("aborted")
		cancel(errTestAbort)
		close(batch)
		err = readPutManyResult(t, errs)
		require.ErrorIs(t, err, storage.ErrBatchAborted, "batch %d", i)
	}
	_, err = db.Get("aborted")
	require.ErrorIs(t, err, storage.ErrNotFound, "aborted batches must not be committed")

	require.NoError(t, db.Shutdown())
}
