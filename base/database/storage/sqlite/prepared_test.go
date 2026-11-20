package sqlite

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnonymizePlaceholders(t *testing.T) {
	t.Parallel()

	q, err := anonymizePlaceholders("INSERT INTO t(a, b) VALUES (?1, ?2), (?3, ?4) RETURNING a", 4)
	require.NoError(t, err)
	assert.Equal(t, "INSERT INTO t(a, b) VALUES (?, ?), (?, ?) RETURNING a", q)

	// Placeholders out of order, missing or of an unexpected count are rejected.
	for _, query := range []string{
		"VALUES (?2, ?1, ?3, ?4)",
		"VALUES (?1, ?2, ?4, ?5)",
		"VALUES (?1, ?2, ?3)",
		"VALUES (?1, ?2, ?3, ?4, ?5)",
		"VALUES (?1, ?2, ?, ?)",
		"VALUES (?1, ?2, ?3, :a)",
	} {
		_, err := anonymizePlaceholders(query, 4)
		assert.Error(t, err, query)
	}
}

func TestBuildUpsertPlaceholders(t *testing.T) {
	t.Parallel()

	db, err := openSQLite("test", t.TempDir(), false)
	require.NoError(t, err)
	defer func() { _ = db.Shutdown() }()

	q, err := db.buildUpsert(upsertRowsPerStatement)
	require.NoError(t, err)
	assert.NotContains(t, q, "?1")
	assert.Contains(t, q, "(?, ?, ?, ?, ?, ?, ?, ?, ?)")
}

func BenchmarkPutMany(b *testing.B) {
	// Configure prepared statement usage.
	origSetting := UsePreparedStatements
	UsePreparedStatements = false
	defer func() {
		UsePreparedStatements = origSetting
	}()

	// Run benchmark.
	benchPutMany(b)
}

func BenchmarkPutManyPreparedStmt(b *testing.B) {
	// Configure prepared statement usage.
	origSetting := UsePreparedStatements
	UsePreparedStatements = true
	defer func() {
		UsePreparedStatements = origSetting
	}()

	// Run benchmark.
	benchPutMany(b)
}

func benchPutMany(b *testing.B) { //nolint:thelper
	// Start database.
	testDir := b.TempDir()
	db, err := openSQLite("test", testDir, false)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		// shutdown
		err = db.Shutdown()
		if err != nil {
			b.Fatal(err)
		}
	}()

	// Start benchmarking.
	b.ResetTimer()

	// Benchmark PutMany.
	records, errs := db.PutMany(false)
	for i := range b.N {
		// Create test record.
		newTestRecord := &TestRecord{
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
		newTestRecord.UpdateMeta()
		newTestRecord.SetKey("test:" + strconv.Itoa(i))

		select {
		case records <- newTestRecord:
		case err := <-errs:
			b.Fatal(err)
		}
	}

	// Finalize.
	close(records)
	err = <-errs
	if err != nil {
		b.Fatal(err)
	}
}

func TestUpsertHasNoReturning(t *testing.T) {
	t.Parallel()

	db := &SQLite{ctx: t.Context()}
	for _, rows := range []int{1, upsertRowsPerStatement} {
		q, err := db.buildUpsert(rows)
		require.NoError(t, err)
		assert.NotContains(t, strings.ToUpper(q), "RETURNING")
		assert.Contains(t, q, "ON CONFLICT")
	}
}
