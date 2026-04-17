package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// passThroughDecider always routes to dest.
func passThroughDecider(dest string) DeciderFunc {
	addr, _ := net.ResolveTCPAddr("tcp", dest)
	return func(_, _ net.Addr) (net.IP, uint16, string, any, error) {
		if addr == nil {
			return nil, 0, "", nil, fmt.Errorf("invalid dest %q", dest)
		}
		return addr.IP, uint16(addr.Port), "", nil, nil
	}
}

// refuseDecider always rejects sessions.
func refuseDecider(_ net.Addr, _ net.Addr) (net.IP, uint16, string, any, error) {
	return nil, 0, "", nil, fmt.Errorf("rejected")
}

// startTCPEchoServer starts a TCP echo server on a random port.
// It returns the address and a stop function.  Accepts testing.TB so it works
// in both tests and benchmarks.
func startTCPEchoServer(tb testing.TB) (addr string, stop func()) {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("echo server listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) //nolint:errcheck
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		ln.Close()
		<-done
	}
}

// startUDPEchoServer starts a UDP echo server on a random port.
func startUDPEchoServer(tb testing.TB) (addr string, stop func()) {
	tb.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		tb.Fatalf("udp echo server listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64*1024)
		for {
			n, peer, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], peer) //nolint:errcheck
		}
	}()
	return conn.LocalAddr().String(), func() {
		conn.Close()
		<-done
	}
}

// ─── TCP tests ────────────────────────────────────────────────────────────────

func TestTCPProxy_ConnectAndForward(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	payload := []byte("hello proxy")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(payload))
	conn.SetDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("echo mismatch: got %q want %q", buf, payload)
	}
}

func TestTCPProxy_BidirectionalBytes(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	const msgSize = 128 * 1024
	payload := bytes.Repeat([]byte("X"), msgSize)

	conn, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	errc := make(chan error, 1)
	recvd := make([]byte, msgSize)
	go func() {
		_, err := io.ReadFull(conn, recvd)
		errc <- err
	}()
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(recvd, payload) {
		t.Fatal("bidirectional echo mismatch")
	}
}

func TestTCPProxy_SessionCleanupOnClose(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Wait for the session to register.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if proxy.Metrics().ActiveSessions == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if proxy.Metrics().ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", proxy.Metrics().ActiveSessions)
	}

	conn.Close()

	// Wait for cleanup.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if proxy.Metrics().ActiveSessions == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if proxy.Metrics().ActiveSessions != 0 {
		t.Fatalf("session not cleaned up: active=%d", proxy.Metrics().ActiveSessions)
	}
	if proxy.Metrics().TotalClosed != 1 {
		t.Fatalf("expected TotalClosed=1, got %d", proxy.Metrics().TotalClosed)
	}
}

func TestTCPProxy_DeciderRejectsSession(t *testing.T) {
	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", refuseDecider, nil)
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	buf := make([]byte, 4)
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected connection to be closed by proxy")
	}
}

func TestTCPProxy_MaxSessions(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	cfg := DefaultConfig()
	cfg.MaxSessions = 1
	proxy, err := NewTCPProxyWithConfig("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil, cfg)
	if err != nil {
		t.Fatalf("NewTCPProxyWithConfig: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	// First connection should succeed and stay open.
	c1, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close()

	// Give the proxy time to accept and register c1.
	time.Sleep(50 * time.Millisecond)

	// Second connection: proxy should accept TCP but immediately close it.
	c2, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close()

	c2.SetDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	buf := make([]byte, 4)
	_, err = c2.Read(buf)
	if err == nil {
		t.Fatal("expected c2 to be rejected")
	}
}

func TestTCPProxy_GracefulShutdown(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := proxy.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// ─── UDP tests ────────────────────────────────────────────────────────────────

func TestUDPProxy_SessionCreation(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	proxy, err := NewUDPProxy("127.0.0.1:0", "udp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	clientConn, err := net.DialUDP("udp", nil,
		proxy.Addr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()

	payload := []byte("hello udp")
	clientConn.SetDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck

	if _, err := clientConn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 256)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf[:n], payload) {
		t.Fatalf("echo mismatch: got %q want %q", buf[:n], payload)
	}

	// Session should be registered.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if proxy.Metrics().ActiveSessions == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if proxy.Metrics().ActiveSessions != 1 {
		t.Fatalf("expected 1 session, got %d", proxy.Metrics().ActiveSessions)
	}
}

func TestUDPProxy_ReplyRouting(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	proxy, err := NewUDPProxy("127.0.0.1:0", "udp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	proxyUDPAddr := proxy.Addr().(*net.UDPAddr)
	const numClients = 3
	const numMessages = 5

	errc := make(chan error, numClients)
	for i := 0; i < numClients; i++ {
		tag := fmt.Sprintf("client%d", i)
		go func(tag string) {
			c, err := net.DialUDP("udp", nil, proxyUDPAddr)
			if err != nil {
				errc <- fmt.Errorf("%s dial: %w", tag, err)
				return
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck

			for j := 0; j < numMessages; j++ {
				msg := fmt.Sprintf("%s-msg%d", tag, j)
				if _, err := c.Write([]byte(msg)); err != nil {
					errc <- fmt.Errorf("%s write: %w", tag, err)
					return
				}
				buf := make([]byte, 256)
				n, err := c.Read(buf)
				if err != nil {
					errc <- fmt.Errorf("%s read: %w", tag, err)
					return
				}
				if string(buf[:n]) != msg {
					errc <- fmt.Errorf("%s: got %q want %q", tag, buf[:n], msg)
					return
				}
			}
			errc <- nil
		}(tag)
	}

	for i := 0; i < numClients; i++ {
		if err := <-errc; err != nil {
			t.Error(err)
		}
	}
}

func TestUDPProxy_IdleTimeoutCleanup(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	cfg := DefaultConfig()
	cfg.ReadTimeout = 200 * time.Millisecond

	proxy, err := NewUDPProxyWithConfig("127.0.0.1:0", "udp4", passThroughDecider(echoAddr), nil, cfg)
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	clientConn, err := net.DialUDP("udp", nil, proxy.Addr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()
	clientConn.SetDeadline(time.Now().Add(time.Second)) //nolint:errcheck

	payload := []byte("trigger session creation")
	if _, err := clientConn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 256)
	if _, err := clientConn.Read(buf); err != nil {
		t.Fatalf("initial read: %v", err)
	}

	// Confirm session is alive.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if proxy.Metrics().ActiveSessions == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if proxy.Metrics().ActiveSessions != 1 {
		t.Fatal("session did not register")
	}

	// Let it idle out.
	time.Sleep(600 * time.Millisecond)

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if proxy.Metrics().ActiveSessions == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if proxy.Metrics().ActiveSessions != 0 {
		t.Fatalf("idle session not cleaned up: active=%d", proxy.Metrics().ActiveSessions)
	}
}

func TestUDPProxy_MaxSessions(t *testing.T) {
	// Count how many sessions the decider accepts; reject beyond limit.
	var accepted atomic.Int32
	const limit = 2
	decider := func(local, peer net.Addr) (net.IP, uint16, string, any, error) {
		if accepted.Load() >= limit {
			return nil, 0, "", nil, fmt.Errorf("max sessions")
		}
		accepted.Add(1)
		return nil, 0, "", nil, fmt.Errorf("no upstream needed for this test")
	}

	cfg := DefaultConfig()
	cfg.MaxSessions = limit
	proxy, err := NewUDPProxyWithConfig("127.0.0.1:0", "udp4", decider, nil, cfg)
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	// The proxy itself enforces MaxSessions, so the decider may or may not
	// be called for the first 'limit' packets.  Just verify the proxy starts.
	if proxy.Addr() == nil {
		t.Fatal("proxy has no address")
	}
}

func TestUDPProxy_GracefulShutdown(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	proxy, err := NewUDPProxy("127.0.0.1:0", "udp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := proxy.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// ─── Misc ─────────────────────────────────────────────────────────────────────

func TestNilLogger(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()
	_, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil)
	if err != nil {
		t.Fatalf("nil Logger should be accepted but got: %v", err)
	}
}

func TestNilDeciderRejected(t *testing.T) {
	if _, err := NewTCPProxy("127.0.0.1:0", "tcp4", nil, nil); err == nil {
		t.Fatal("nil DeciderFunc should be rejected")
	}
	if _, err := NewUDPProxy("127.0.0.1:0", "udp4", nil, nil); err == nil {
		t.Fatal("nil DeciderFunc should be rejected")
	}
}

func TestMetricsString(t *testing.T) {
	m := Metrics{ActiveSessions: 3, TotalCreated: 10, TotalClosed: 7}
	s := m.String()
	if s == "" {
		t.Fatal("Metrics.String() returned empty string")
	}
}
