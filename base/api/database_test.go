package api

import (
	"testing"
	"time"

	"github.com/tevino/abool"
)

// TestDatabaseWebsocketQueueMessageShutdown checks that senders do not block
// forever on a full send queue after the connection was shut down.
func TestDatabaseWebsocketQueueMessageShutdown(t *testing.T) {
	t.Parallel()

	wsAPI := &DatabaseWebsocketAPI{
		DatabaseAPI: DatabaseAPI{
			shutdownSignal: make(chan struct{}),
			shuttingDown:   abool.NewBool(false),
		},
		sendQueue: make(chan []byte, 1),
	}

	// Fill the queue, there is no writer.
	wsAPI.queueMessage([]byte("a"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		wsAPI.queueMessage([]byte("b"))
	}()

	// The sender must block while the connection is alive.
	select {
	case <-done:
		t.Fatal("sender did not wait for space in the send queue")
	case <-time.After(50 * time.Millisecond):
	}

	// The sender must be released when the connection shuts down.
	close(wsAPI.shutdownSignal)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sender is still blocked after shutdown")
	}
}
