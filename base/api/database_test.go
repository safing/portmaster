package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
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

// TestDatabaseWebsocketWriteTimeout checks that the websocket writer gives up
// on a peer that keeps the connection open but stopped reading, and shuts the
// connection down instead of blocking forever.
func TestDatabaseWebsocketWriteTimeout(t *testing.T) { //nolint:paralleltest // Modifies package variable.
	oldTimeout := dbAPIWriteTimeout
	dbAPIWriteTimeout = 200 * time.Millisecond
	defer func() { dbAPIWriteTimeout = oldTimeout }()

	// Server side: upgrade and hand over the connection.
	serverConns := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{WriteBufferSize: 65536}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		serverConns <- c
	}))
	defer srv.Close()

	// Client side: connect, but never read.
	clientConn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	defer func() { _ = clientConn.Close() }()

	var serverConn *websocket.Conn
	select {
	case serverConn = <-serverConns:
	case <-time.After(5 * time.Second):
		t.Fatal("websocket upgrade did not complete")
	}

	api := &DatabaseWebsocketAPI{
		DatabaseAPI: DatabaseAPI{
			queries:        make(map[string]*iterator.Iterator),
			subs:           make(map[string]*database.Subscription),
			shutdownSignal: make(chan struct{}),
			shuttingDown:   abool.NewBool(false),
		},
		sendQueue: make(chan []byte, 100),
		conn:      serverConn,
	}
	module.mgr.Go("database api writer test", api.writer)

	// Feed messages until the socket buffers are full and the writer blocks.
	// The writer must then time out and shut the connection down.
	msg := make([]byte, 1<<20)
	giveUp := time.After(10 * time.Second)
	for {
		select {
		case api.sendQueue <- msg:
		case <-api.shutdownSignal:
			if !api.shuttingDown.IsSet() {
				t.Fatal("shutdown signal closed without shutting down")
			}
			return
		case <-giveUp:
			t.Fatal("websocket writer blocked on a peer that stopped reading")
		}
	}
}
