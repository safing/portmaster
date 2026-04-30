package proxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// ─── TCP benchmarks ───────────────────────────────────────────────────────────

// BenchmarkTCPProxy_Throughput measures round-trip throughput through the TCP
// proxy using a local echo server.  Run with -benchmem to observe allocations.
func BenchmarkTCPProxy_Throughput(b *testing.B) {
	echoAddr, stopEcho := startTCPEchoServer(b)
	defer stopEcho()

	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil, "")
	if err != nil {
		b.Fatalf("NewTCPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Minute)) //nolint:errcheck

	const msgSize = 32 * 1024
	payload := bytes.Repeat([]byte("B"), msgSize)
	recv := make([]byte, msgSize)

	b.SetBytes(int64(msgSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := conn.Write(payload); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := io.ReadFull(conn, recv); err != nil {
			b.Fatalf("read: %v", err)
		}
	}
}

// BenchmarkTCPProxy_NewSession measures the overhead of establishing and
// tearing down a new TCP session through the proxy.
func BenchmarkTCPProxy_NewSession(b *testing.B) {
	echoAddr, stopEcho := startTCPEchoServer(b)
	defer stopEcho()

	proxy, err := NewTCPProxy("127.0.0.1:0", "tcp4", passThroughDecider(echoAddr), nil, "")
	if err != nil {
		b.Fatalf("NewTCPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := net.DialTimeout("tcp", proxy.Addr().String(), 2*time.Second)
		if err != nil {
			b.Fatalf("dial: %v", err)
		}
		conn.Close()
		// Allow the session to be removed before next iteration.
		for proxy.Metrics().TotalClosed < uint64(i+1) {
			// spin — in benchmarks this is acceptable over a sleep
		}
	}
}

// ─── UDP benchmarks ───────────────────────────────────────────────────────────

// BenchmarkUDPProxy_Throughput measures datagrams-per-second through the UDP
// proxy.
func BenchmarkUDPProxy_Throughput(b *testing.B) {
	echoAddr, stopEcho := startUDPEchoServer(b)
	defer stopEcho()

	cfg := DefaultConfig()
	cfg.ReadTimeout = 30 * time.Second
	proxy, err := NewUDPProxyWithConfig("127.0.0.1:0", "udp4", passThroughDecider(echoAddr), nil, cfg, "")
	if err != nil {
		b.Fatalf("NewUDPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	clientConn, err := net.DialUDP("udp", nil, proxy.Addr().(*net.UDPAddr))
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()
	clientConn.SetDeadline(time.Now().Add(10 * time.Minute)) //nolint:errcheck

	const msgSize = 1024 * (64 - 1)
	payload := bytes.Repeat([]byte("U"), msgSize)
	recv := make([]byte, 64*1024)

	b.SetBytes(int64(msgSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := clientConn.Write(payload); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := clientConn.Read(recv); err != nil {
			b.Fatalf("read: %v", err)
		}
	}
}

// BenchmarkUDPProxy_NewSession measures session-creation cost for the UDP
// proxy: each iteration uses a unique local port so that every packet triggers
// the slow-path decider call and upstream dial.
func BenchmarkUDPProxy_NewSession(b *testing.B) {
	echoAddr, stopEcho := startUDPEchoServer(b)
	defer stopEcho()

	cfg := DefaultConfig()
	cfg.ReadTimeout = 100 * time.Millisecond
	proxy, err := NewUDPProxyWithConfig("127.0.0.1:0", "udp4", passThroughDecider(echoAddr), nil, cfg, "")
	if err != nil {
		b.Fatalf("NewUDPProxy: %v", err)
	}
	defer proxy.Shutdown(context.Background()) //nolint:errcheck

	proxyUDPAddr := proxy.Addr().(*net.UDPAddr)
	payload := []byte("ping")
	recv := make([]byte, 64)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c, err := net.DialUDP("udp", nil, proxyUDPAddr)
		if err != nil {
			b.Fatalf("dial: %v", err)
		}
		c.SetDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
		if _, err := c.Write(payload); err != nil {
			c.Close()
			b.Fatalf("write: %v", err)
		}
		if _, err := c.Read(recv); err != nil {
			c.Close()
			b.Fatalf("read: %v", err)
		}
		c.Close()
	}
}

// startTCPEchoServer and startUDPEchoServer are defined in proxy_test.go.
