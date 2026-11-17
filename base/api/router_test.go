package api

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServerReadHeaderTimeout checks that the API server closes connections
// that do not complete their request headers in time (slowloris).
func TestServerReadHeaderTimeout(t *testing.T) { //nolint:paralleltest // Modifies package variable.
	// The server used for serving must be configured with the timeout.
	if server.ReadHeaderTimeout != serverReadHeaderTimeout || server.ReadHeaderTimeout <= 0 {
		t.Fatalf("API server has read header timeout %s, expected %s", server.ReadHeaderTimeout, serverReadHeaderTimeout)
	}

	// Run a server configured the same way, with a lower timeout.
	oldTimeout := serverReadHeaderTimeout
	serverReadHeaderTimeout = 200 * time.Millisecond
	defer func() { serverReadHeaderTimeout = oldTimeout }()

	srv := newServer()
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	// Send an incomplete request header and then stall.
	start := time.Now()
	_, err = io.WriteString(conn, "GET /api/v1/ping HTTP/1.1\r\nHost: 127.0.0.1\r\nX-Slow: ")
	if err != nil {
		t.Fatal(err)
	}

	// The server must close the connection after the timeout.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = io.Copy(io.Discard, conn)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("connection was not closed by the server after %s: %s", elapsed, err)
	}
	if elapsed < serverReadHeaderTimeout {
		t.Fatalf("connection was closed after %s, before the read header timeout", elapsed)
	}
	t.Logf("connection closed by the server after %s", elapsed)
}
