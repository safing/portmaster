package netquery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/runtime"
)

// blockingStore is a ConnectionStore whose Save blocks until it is released.
type blockingStore struct {
	ConnectionStore

	entered chan struct{}
	release chan struct{}
	saved   chan Conn
}

func (s *blockingStore) Save(_ context.Context, conn Conn, _ bool) error {
	s.entered <- struct{}{}
	<-s.release
	s.saved <- conn
	return nil
}

// TestHandleConnectionSavesUnlocked checks that the connection is not locked
// while it is written to the store, and that the saved model is a snapshot
// that does not change with the connection anymore.
func TestHandleConnectionSavesUnlocked(t *testing.T) {
	t.Parallel()

	store := &blockingStore{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		saved:   make(chan Conn, 1),
	}
	mng, err := NewManager(store, "netquery/conns/", runtime.NewRegistry())
	require.NoError(t, err)

	conn := newBenchNetworkConn(t, "save-unlocked")
	conn.ConnectionEstablished = false

	done := make(chan struct{})
	go func() {
		defer close(done)
		mng.handleConnection(context.Background(), conn)
	}()

	select {
	case <-store.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("connection was not saved")
	}

	// The packet path must be able to lock and change the connection while
	// the (slow) store is writing.
	locked := make(chan struct{})
	go func() {
		conn.Lock()
		conn.ConnectionEstablished = true
		conn.Ended = 1700000100
		conn.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		close(store.release)
		<-done
		t.Fatal("connection is locked while it is saved to the store")
	}

	close(store.release)
	<-done

	saved := <-store.saved
	require.NotNil(t, saved.Allowed)
	assert.False(t, *saved.Allowed, "saved model changed with the connection")
	assert.Nil(t, saved.Ended, "saved model changed with the connection")
	assert.True(t, saved.Active)
}

// BenchmarkSnapshotConnection measures the part of handleConnection that
// holds the lock of the connection. Before saving was moved out of the lock,
// the lock was held for all of BenchmarkHandleConnection.
func BenchmarkSnapshotConnection(b *testing.B) {
	conn := newBenchNetworkConn(b, "bench")

	b.ReportAllocs()
	for b.Loop() {
		_, _, _, ok := snapshotConnection(conn)
		if !ok {
			b.Fatal("connection not complete")
		}
	}
}
