package badger

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

var (
	// Compile time interface checks.
	_ storage.Interface  = &Badger{}
	_ storage.Maintainer = &Badger{}
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

func TestBadger(t *testing.T) {
	t.Parallel()

	testDir, err := os.MkdirTemp("", "testing-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(testDir) // clean up
	}()

	// start
	db, err := NewBadger("test", testDir)
	if err != nil {
		t.Fatal(err)
	}

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

	if !reflect.DeepEqual(a, a1) {
		t.Fatalf("mismatch, got %v", a1)
	}

	// test query
	q := query.New("").MustBeValid()
	it, err := db.Query(q, true, true)
	if err != nil {
		t.Fatal(err)
	}
	cnt := 0
	for range it.Next {
		cnt++
	}
	if it.Err() != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
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

	// maintenance
	maintainer, ok := db.(storage.Maintainer)
	if ok {
		err = maintainer.Maintain(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		err = maintainer.MaintainThorough(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("should implement Maintainer")
	}

	// shutdown
	err = db.Shutdown()
	if err != nil {
		t.Fatal(err)
	}
}
