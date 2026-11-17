package network

import (
	"strconv"
	"testing"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
)

// TestProcessQueryCancel checks that a query that is canceled by the consumer
// (eg. because the UI disconnected) does not block the query worker forever.
func TestProcessQueryCancel(t *testing.T) { //nolint:paralleltest // Modifies the global connection store.
	// Not parallel: Modifies the global connection store.

	// Add more connections than fit into the iterator buffer.
	added := make([]*Connection, 0, 100)
	for i := range 100 {
		conn := &Connection{
			ID:           "query-cancel-test-" + strconv.Itoa(i),
			dataComplete: abool.NewBool(true),
		}
		conn.SetKey(makeKey(1, dbScopeIP, conn.ID))
		conns.add(conn)
		added = append(added, conn)
	}
	defer func() {
		for _, conn := range added {
			conns.delete(conn)
		}
	}()

	it := iterator.New()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		(&StorageInterface{}).processQuery(query.New("network:tree/"), it)
	}()

	// Read one record, then abandon the query.
	select {
	case <-it.Next:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not return any records")
	}
	it.Cancel()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("query worker is still blocked after the query was canceled")
	}
}
