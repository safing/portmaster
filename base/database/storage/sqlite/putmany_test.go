package sqlite

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/structures/dsd"
)

// runPutManyVariants runs the test with and without prepared statements.
// Callers must not be parallel, as this switches the package-wide
// UsePreparedStatements setting.
func runPutManyVariants(t *testing.T, test func(t *testing.T)) {
	t.Helper()

	origSetting := UsePreparedStatements
	t.Cleanup(func() { UsePreparedStatements = origSetting })

	for _, usePrepared := range []bool{true, false} {
		t.Run("prepared="+strconv.FormatBool(usePrepared), func(t *testing.T) {
			UsePreparedStatements = usePrepared
			test(t)
		})
	}
}

func newPutManyTestRecord(key string) *TestRecord {
	r := &TestRecord{S: "batch"}
	r.SetKey("test:" + key)
	r.UpdateMeta()
	return r
}

// readPutManyResult reads the result of a PutMany batch with a timeout.
func readPutManyResult(t *testing.T, errs <-chan error) error {
	t.Helper()

	select {
	case err := <-errs:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the PutMany result")
		return nil
	}
}

// TestPutManyWriteError checks that a failed write is reported, rolls back
// the batch and releases the database.
func TestPutManyWriteError(t *testing.T) { //nolint:paralleltest // Switches package-wide setting.
	runPutManyVariants(t, testPutManyWriteError)
}

func testPutManyWriteError(t *testing.T) {
	t.Helper()

	db, err := openSQLite("test", t.TempDir(), false)
	require.NoError(t, err)

	// Write a valid record and one that cannot be serialized.
	badRecord, err := record.NewWrapper("test:bad", &record.Meta{}, 255, []byte("bad"))
	require.NoError(t, err)
	batch, errs := db.PutMany(false)
	batch <- newPutManyTestRecord("good")
	batch <- badRecord

	// Wait for the result without reading it.
	require.Eventually(t, func() bool { return len(errs) > 0 }, 10*time.Second, time.Millisecond)

	// The transaction must have been rolled back already, releasing the
	// (single) database connection, even if the caller has not read the
	// result yet.
	getDone := make(chan error, 1)
	go func() {
		_, err := db.Get("good")
		getDone <- err
	}()
	var result error
	select {
	case err := <-getDone:
		require.ErrorIs(t, err, storage.ErrNotFound, "batch must not be committed")
		result = readPutManyResult(t, errs)
	case <-time.After(5 * time.Second):
		t.Error("database is blocked by the failed batch until its result is read")
		// Read the result to unblock the database and finish the test.
		result = readPutManyResult(t, errs)
		<-getDone
	}
	require.ErrorIs(t, result, dsd.ErrIncompatibleFormat)

	// Exactly one result must be sent.
	select {
	case err := <-errs:
		t.Fatalf("received second result: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, db.Shutdown())
}

// TestPutManyShutdown checks that a batch aborted by a shutdown of the
// database does not report success.
func TestPutManyShutdown(t *testing.T) { //nolint:paralleltest // Switches package-wide setting.
	runPutManyVariants(t, testPutManyShutdown)
}

func testPutManyShutdown(t *testing.T) {
	t.Helper()

	// Repeat, as the outcome before the fix depended on timing.
	for i := range 20 {
		db, err := openSQLite("test", t.TempDir(), false)
		require.NoError(t, err)

		batch, errs := db.PutMany(false)
		batch <- newPutManyTestRecord("aborted")
		db.cancelCtx()
		err = readPutManyResult(t, errs)
		assert.Error(t, err, "aborted batch %d must not report success", i)
		assert.False(t, errors.Is(err, dsd.ErrIncompatibleFormat))

		require.NoError(t, db.Shutdown())
	}
}
