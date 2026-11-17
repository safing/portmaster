package sqlite

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

var (
	// Compile time interface checks.
	_ storage.Interface = &SQLite{}
	_ storage.Batcher   = &SQLite{}
	_ storage.Purger    = &SQLite{}
)

type TestRecord struct {
	record.Base
	sync.Mutex
	S    string
	I    int
	I8   int8
	I16  int16
	I32  int32
	I64  int64
	UI   uint
	UI8  uint8
	UI16 uint16
	UI32 uint32
	UI64 uint64
	F32  float32
	F64  float64
	B    bool
}

func TestSQLite(t *testing.T) {
	t.Parallel()

	// start
	testDir := t.TempDir()
	db, err := openSQLite("test", testDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// shutdown
		err = db.Shutdown()
		if err != nil {
			t.Fatal(err)
		}
	}()

	a := &TestRecord{
		S:    "banana",
		I:    42,
		I8:   42,
		I16:  42,
		I32:  42,
		I64:  42,
		UI:   42,
		UI8:  42,
		UI16: 42,
		UI32: 42,
		UI64: 42,
		F32:  42.42,
		F64:  42.42,
		B:    true,
	}
	a.SetMeta(&record.Meta{})
	a.Meta().Update()
	a.SetKey("test:A")

	// put record
	_, err = db.Put(a)
	if err != nil {
		t.Fatal(err)
	}

	// get and compare
	r1, err := db.Get("A")
	if err != nil {
		t.Fatal(err)
	}

	a1 := &TestRecord{}
	err = record.Unwrap(r1, a1)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, a, a1, "struct must match")

	// setup query test records
	qA := &TestRecord{}
	qA.SetKey("test:path/to/A")
	qA.UpdateMeta()

	qB := &TestRecord{}
	qB.SetKey("test:path/to/B")
	qB.UpdateMeta()
	// Set creation/modification in the past.
	qB.Meta().Created = time.Now().Add(-time.Hour).Unix()
	qB.Meta().Modified = time.Now().Add(-time.Hour).Unix()

	qC := &TestRecord{}
	qC.SetKey("test:path/to/C")
	qC.UpdateMeta()
	// Set expiry in the past.
	qC.Meta().Expires = time.Now().Add(-time.Hour).Unix()

	qZ := &TestRecord{}
	qZ.SetKey("test:z")
	qZ.UpdateMeta()

	put, errs := db.PutMany(false)
	put <- qA
	put <- qB
	put <- qC
	put <- qZ
	close(put)
	err = <-errs
	if err != nil {
		t.Fatal(err)
	}

	// test query
	q := query.New("test:path/to/").MustBeValid()
	it, err := db.Query(q, true, true)
	if err != nil {
		t.Fatal(err)
	}
	cnt := 0
	for range it.Next {
		cnt++
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if cnt != 2 {
		// Note: One is expired.
		t.Fatalf("unexpected query result count: %d", cnt)
	}

	// delete
	err = db.Delete("A")
	if err != nil {
		t.Fatal(err)
	}

	// check if its gone
	_, err = db.Get("A")
	if err == nil {
		t.Fatal("should fail")
	}

	// purge older than
	n, err := db.PurgeOlderThan(t.Context(), "path/to/", time.Now().Add(-30*time.Minute), true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("unexpected purge older than delete count: %d", n)
	}

	// maintenance
	err = db.MaintainRecordStates(t.Context(), time.Now().Add(-time.Minute), true)
	if err != nil {
		t.Fatal(err)
	}

	// maintenance
	err = db.MaintainRecordStates(t.Context(), time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}

	// purge
	n, err = db.Purge(t.Context(), query.New("test:path/to/").MustBeValid(), true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("unexpected purge delete count: %d", n)
	}

	// Maintenance
	err = db.Maintain(t.Context())
	if err != nil {
		t.Fatalf("Maintain: %s", err)
	}
	err = db.MaintainThorough(t.Context())
	if err != nil {
		t.Fatalf("MaintainThorough: %s", err)
	}

	// test query
	q = query.New("test").MustBeValid()
	it, err = db.Query(q, true, true)
	if err != nil {
		t.Fatal(err)
	}
	cnt = 0
	for range it.Next {
		cnt++
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if cnt != 1 {
		t.Fatalf("unexpected query result count: %d", cnt)
	}
}

func TestMigrationTimezoneRoundTrip(t *testing.T) { //nolint:paralleltest
	// We intentionally do not call t.Parallel() here,
	// because this test changes the global time.Local variable, which can affect other tests if run in parallel.

	// Simulate a fixed-offset zone: Go abbreviates it "UTC+2", which contains digits
	// that cause time.Parse("... MST", ...) to fail when sql-migrate reads back the
	// applied_at timestamp it wrote into gorp_migrations on the previous open.
	origLocal := time.Local //nolint:gosmopolitan
	time.Local = time.FixedZone("UTC+2", 2*60*60) //nolint:gosmopolitan
	t.Cleanup(func() { time.Local = origLocal })   //nolint:gosmopolitan

	testDir := t.TempDir()

	// First open: writes the gorp_migrations timestamp.
	db, err := openSQLite("test", testDir, false)
	require.NoError(t, err)
	require.NoError(t, db.Shutdown())

	// Second open: sql-migrate reads gorp_migrations to check which migrations ran.
	// Without the fix that SELECT scan crashes with:
	// "unsupported Scan, storing driver.Value type string into type *time.Time"
	db, err = openSQLite("test", testDir, false)
	require.NoError(t, err)
	require.NoError(t, db.Shutdown())
}

func TestPutManyBatches(t *testing.T) { //nolint:paralleltest
	// Not parallel: this test switches the package-wide UsePreparedStatements
	// setting, which other tests using PutMany would observe.
	origSetting := UsePreparedStatements
	t.Cleanup(func() { UsePreparedStatements = origSetting })

	for _, usePrepared := range []bool{true, false} { //nolint:paralleltest
		t.Run("prepared="+strconv.FormatBool(usePrepared), func(t *testing.T) {
			UsePreparedStatements = usePrepared
			testPutManyBatches(t)
		})
	}
}

func testPutManyBatches(t *testing.T) {
	t.Helper()

	db, err := openSQLite("test", t.TempDir(), false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Shutdown())
	}()

	// Write two full statements and a partial one.
	const count = upsertRowsPerStatement*2 + 20
	putRecords := func(value string) {
		put, errs := db.PutMany(false)
		for i := range count {
			r := &TestRecord{S: value, I: i}
			r.SetKey("test:batch/" + strconv.Itoa(i))
			r.UpdateMeta()
			put <- r
		}
		close(put)
		require.NoError(t, <-errs)
	}
	putRecords("first")

	// Writing the same keys again must update the records.
	putRecords("second")

	for i := range count {
		r, err := db.Get("batch/" + strconv.Itoa(i))
		require.NoError(t, err)
		got := &TestRecord{}
		require.NoError(t, record.Unwrap(r, got))
		assert.Equal(t, "second", got.S)
		assert.Equal(t, i, got.I)
	}
}

// TestQueryConsumerUsesSameDB checks that a query consumer can read from and
// write to the same database while ranging over the query iterator, as the
// profile migrations do. A running query must not hold the (single) SQLite
// connection while it waits for the consumer.
func TestQueryConsumerUsesSameDB(t *testing.T) {
	t.Parallel()

	db, err := openSQLite("test", t.TempDir(), false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Shutdown())
	}()

	// More records than fit into the iterator buffer and more than one query page.
	const count = queryPageSize*2 + 50
	for i := range count {
		r := &TestRecord{S: "orig", I: i}
		r.SetKey("test:consumer/" + strconv.Itoa(i))
		r.UpdateMeta()
		_, err := db.Put(r)
		require.NoError(t, err)
	}

	it, err := db.Query(query.New("test:consumer/").MustBeValid(), true, true)
	require.NoError(t, err)

	var (
		start      = time.Now()
		seen       = make(map[string]struct{}, count)
		slowestOp  time.Duration
		nestedErrs int
	)
	for r := range it.Next {
		seen[r.DatabaseKey()] = struct{}{}

		opStart := time.Now()
		_, err := db.Get(r.DatabaseKey())
		if err != nil {
			nestedErrs++
		}
		updated := &TestRecord{S: "updated"}
		updated.SetKey(r.Key())
		updated.UpdateMeta()
		_, err = db.Put(updated)
		if err != nil {
			nestedErrs++
		}
		slowestOp = max(slowestOp, time.Since(opStart))
	}
	require.NoError(t, it.Err())
	assert.Len(t, seen, count, "all records must be delivered")
	assert.Zero(t, nestedErrs, "nested Get/Put must succeed")
	assert.Less(t, slowestOp, 250*time.Millisecond, "nested Get/Put blocked by running query")
	assert.Less(t, time.Since(start), 5*time.Second)

	// Writes from the consumer must have been applied.
	r, err := db.Get("consumer/0")
	require.NoError(t, err)
	got := &TestRecord{}
	require.NoError(t, record.Unwrap(r, got))
	assert.Equal(t, "updated", got.S)
}
