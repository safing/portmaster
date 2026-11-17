package hashmap

import (
	"fmt"
	"testing"
	"time"

	"github.com/safing/portmaster/base/database/query"
)

// TestQueryDoesNotBlockWriters checks that a query whose consumer is stalled
// does not hold the storage lock and thereby block writers (and, through
// the waiting writer, all other readers).
func TestQueryDoesNotBlockWriters(t *testing.T) {
	t.Parallel()

	db, err := NewHashMap("test", "")
	if err != nil {
		t.Fatal(err)
	}

	// More records than the iterator buffer holds, so the query executor
	// has to wait for the consumer.
	const total = 50
	for i := range total {
		r := &TestRecord{}
		r.SetKey(fmt.Sprintf("test:q/%d", i))
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

	// A writer and a reader must not be blocked by the stalled query.
	// The query executor gives up a send after 1s, so stay well below it.
	done := make(chan error, 1)
	go func() {
		w := &TestRecord{}
		w.SetKey("test:other")
		w.CreateMeta()
		_, err := db.Put(w)
		if err == nil {
			_, err = db.Get("other")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("writer blocked by a query with a stalled consumer")
	}

	// Resume consuming: all matching records must still be delivered.
	cnt := 0
	for range it.Next {
		cnt++
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if cnt != total {
		t.Fatalf("unexpected query result count: %d", cnt)
	}
}
