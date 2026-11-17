package proxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// TestTCPProxy_OneWayTransferOutlivesReadTimeout checks that a session which
// is only active in one direction (a long upload with a silent server) is not
// treated as idle. The server answers only after the whole upload was
// received; that answer must reach the client.
func TestTCPProxy_OneWayTransferOutlivesReadTimeout(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	const (
		chunks   = 12
		interval = 40 * time.Millisecond
	)
	serverErr := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer c.Close() //nolint:errcheck
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		// Read the full upload (until the client half-closes), then answer.
		n, err := io.Copy(io.Discard, c)
		if err != nil {
			serverErr <- err
			return
		}
		if n != chunks {
			_, _ = c.Write([]byte("SHORT"))
			serverErr <- nil
			return
		}
		_, err = c.Write([]byte("OK"))
		serverErr <- err
	}()

	cfg := DefaultConfig()
	cfg.ReadTimeout = 150 * time.Millisecond
	p, err := NewTCPProxyWithConfig("127.0.0.1:0", "tcp4", passThroughDecider(ln.Addr().String()), nil, cfg, "")
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer p.Shutdown(context.Background()) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", p.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Upload for about 3x ReadTimeout while the server stays silent.
	for range chunks {
		_, err = conn.Write([]byte{'x'})
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(interval)
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatalf("unexpected conn type %T", conn)
	}
	err = tcpConn.CloseWrite()
	if err != nil {
		t.Fatalf("close write: %v", err)
	}

	reply, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if string(reply) != "OK" {
		t.Fatalf("got reply %q, want %q", reply, "OK")
	}
	err = <-serverErr
	if err != nil {
		t.Fatalf("server: %v", err)
	}
}

// TestTCPProxy_IdleSessionTimesOut checks that a session without traffic in
// either direction is still closed after ReadTimeout.
func TestTCPProxy_IdleSessionTimesOut(t *testing.T) {
	t.Parallel()

	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	cfg := DefaultConfig()
	cfg.ReadTimeout = 100 * time.Millisecond
	p, err := NewTCPProxyWithConfig("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil, cfg, "")
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer p.Shutdown(context.Background()) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", p.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// The proxy must close the idle session, which the client sees as EOF.
	_, err = conn.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("expected idle session to be closed")
	}
	deadline := time.Now().Add(time.Second)
	for p.Metrics().ActiveSessions != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if active := p.Metrics().ActiveSessions; active != 0 {
		t.Fatalf("idle session not cleaned up: active=%d", active)
	}
}
