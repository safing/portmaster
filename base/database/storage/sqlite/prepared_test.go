package sqlite

import (
	"strconv"
	"testing"
)

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
