package hashmap

import (
	"reflect"
	"testing"

	"github.com/safing/portmaster/base/db"
	"github.com/safing/portmaster/base/db/query"
	"github.com/safing/structures/dsd"
)

// Compile time interface checks.
var _ db.Database = &HashMapDB{}

type TestData struct { //nolint:maligned
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

func TestHashMap(t *testing.T) {
	t.Parallel()

	// start
	testDB := New("test")
	err := testDB.Start()
	if err != nil {
		t.Fatal(err)
	}

	dataA := &TestData{
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
	recA, err := db.NewRecord("A", db.PermitSelf, dsd.JSON, dataA)
	if err != nil {
		t.Fatal(err)
	}

	// put record
	err = testDB.Put(recA)
	if err != nil {
		t.Fatal(err)
	}

	// get and compare
	recA1, err := testDB.Get("A")
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(dataA, recA1.Object()) {
		t.Fatalf("mismatch, got %v", recA1.Object())
	}

	// setup query test records
	qA, err := db.NewRecord("path/to/A", db.PermitAnyone, dsd.JSON, &TestData{})
	if err != nil {
		t.Fatal(err)
	}
	qB, err := db.NewRecord("path/to/B", db.PermitAnyone, dsd.JSON, &TestData{})
	if err != nil {
		t.Fatal(err)
	}
	qC, err := db.NewRecord("path/to/C", db.PermitAnyone, dsd.JSON, &TestData{})
	if err != nil {
		t.Fatal(err)
	}
	qZ, err := db.NewRecord("z", db.PermitAnyone, dsd.JSON, &TestData{})
	if err != nil {
		t.Fatal(err)
	}
	qNo, err := db.NewRecord("path/to/no", db.PermitSelf, dsd.JSON, &TestData{})
	if err != nil {
		t.Fatal(err)
	}
	// put
	err = testDB.Put(qA)
	if err == nil {
		err = testDB.Put(qB)
	}
	if err == nil {
		err = testDB.Put(qC)
	}
	if err == nil {
		err = testDB.Put(qZ)
	}
	if err == nil {
		err = testDB.Put(qNo)
	}
	if err != nil {
		t.Fatal(err)
	}

	// test query
	q := query.New("path/to/")
	q.SetAccessPermission(db.PermitUser)
	it, err := testDB.Query(q, 10)
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
	if cnt != 3 {
		t.Fatalf("unexpected query result count: %d", cnt)
	}

	// delete
	err = testDB.Delete("A")
	if err != nil {
		t.Fatal(err)
	}

	// check if its gone
	_, err = testDB.Get("A")
	if err == nil {
		t.Fatal("should fail")
	}

	// shutdown
	err = testDB.Stop()
	if err != nil {
		t.Fatal(err)
	}
}
