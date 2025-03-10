package sqlite

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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

type TestRecord struct { //nolint:maligned
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
