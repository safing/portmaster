package bbolt

import (
	"fmt"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

// TestQueryDoesNotBlockWriters checks that a query whose consumer is stalled
// does not keep its bbolt read transaction open. A write that has to grow the
// mmap waits for all read transactions to finish, so an open read transaction
// would block it (and with it all other writers and new readers).
func TestQueryDoesNotBlockWriters(t *testing.T) {
	t.Parallel()

	db, err := NewBBolt("test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.Shutdown()
	}()

	// More records than the iterator buffer holds, so the query executor
	// has to wait for the consumer, and more than one query page.
	const (
		total     = queryPageSize*2 + 50
		testValue = "queried"
	)
	for i := range total {
		r := &TestRecord{S: testValue}
		r.SetKey(fmt.Sprintf("test:q/%04d", i))
		r.CreateMeta()
		_, err := db.Put(r)
		if err != nil {
			t.Fatal(err)
		}
	}

	it, err := db.Query(query.New("test:q/").MustBeValid(), true, true)
	if err != nil {
		t.Fatal(err)
	}

	// Consumer stalls: wait until the iterator buffer is full.
	deadline := time.Now().Add(time.Second)
	for len(it.Next) < cap(it.Next) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	// Write a record that is larger than the current mmap (32KiB initially),
	// so that bbolt has to remap the database file. The remap waits for all
	// read transactions to finish. The query executor gives up a send after
	// 1s, so stay well below it.
	done := make(chan error, 1)
	go func() {
		w := &TestRecord{S: string(make([]byte, 256<<10))}
		w.SetKey("test:big")
		w.CreateMeta()
		_, err := db.Put(w)
		if err == nil {
			_, err = db.Get("big")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("writer blocked by a query with a stalled consumer")
	}

	// Resume consuming: all matching records must still be delivered, in
	// key order.
	cnt := 0
	for r := range it.Next {
		if want := fmt.Sprintf("q/%04d", cnt); r.DatabaseKey() != want {
			t.Fatalf("unexpected record %q, want %q", r.DatabaseKey(), want)
		}
		// The records must own their data, as the read transaction (and with
		// it the mmapped data) is gone by now.
		got := &TestRecord{}
		err := record.Unwrap(r, got)
		if err != nil {
			t.Fatal(err)
		}
		if got.S != testValue {
			t.Fatalf("unexpected record data: %q", got.S)
		}
		cnt++
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if cnt != total {
		t.Fatalf("unexpected query result count: %d", cnt)
	}
}
