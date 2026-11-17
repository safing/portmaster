package sluice

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// startTestSluice starts a sluice on loopback and returns it once it listens.
func startTestSluice(t *testing.T, networkName string) (*Sluice, net.Addr) {
	t.Helper()

	setTestModule(t)

	StartSluice(networkName, "127.0.0.1:0")
	var s *Sluice
	waitFor(t, func() bool {
		var ok bool
		s, ok = getSluice(networkName)
		return ok
	})
	s.lock.Lock()
	defer s.lock.Unlock()
	return s, s.listener.Addr()
}

func setDeadline(t *testing.T, conn net.Conn) {
	t.Helper()

	err := conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
}

func TestUDPSluiceLifecycle(t *testing.T) { //nolint:paralleltest // Uses global module state.
	_, sluiceAddr := startTestSluice(t, "udp4")

	// Prepare app socket.
	app, err := net.DialUDP("udp4", nil, sluiceAddr.(*net.UDPAddr)) //nolint:forcetypeassert
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	setDeadline(t, app)
	appAddr := app.LocalAddr().(*net.UDPAddr) //nolint:forcetypeassert

	// Register request for the app socket.
	accepted := make(chan net.Conn, 1)
	err = AwaitRequest(
		testConnInfo(packet.UDP, appAddr.IP, uint16(appAddr.Port)), //nolint:gosec // Port is always in range.
		func(_ *network.Connection, conn net.Conn) { accepted <- conn },
	)
	if err != nil {
		t.Fatal(err)
	}

	// Send first packet and get connection.
	_, err = app.Write([]byte("ping"))
	if err != nil {
		t.Fatal(err)
	}
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("request callback not called")
	}
	if conn == nil {
		t.Fatal("got nil conn")
	}

	// Read first packet.
	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "ping" {
		t.Fatalf("unexpected read: %q, %v", buf[:n], err)
	}

	// Reply.
	_, err = conn.Write([]byte("pong"))
	if err != nil {
		t.Fatal(err)
	}
	n, err = app.Read(buf)
	if err != nil || string(buf[:n]) != "pong" {
		t.Fatalf("unexpected app read: %q, %v", buf[:n], err)
	}

	// Read a packet in parts.
	_, err = app.Write([]byte("abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	small := make([]byte, 4)
	n, err = conn.Read(small)
	if err != nil || string(small[:n]) != "abcd" {
		t.Fatalf("unexpected partial read: %q, %v", small[:n], err)
	}
	n, err = conn.Read(small)
	if err != nil || string(small[:n]) != "ef" {
		t.Fatalf("unexpected rest read: %q, %v", small[:n], err)
	}

	// Close connection.
	err = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after close, got %v", err)
	}
	_, err = conn.Write([]byte("x"))
	if err == nil {
		t.Fatal("expected error when writing to closed conn")
	}
}

func TestUDPSluiceWithoutRequest(t *testing.T) { //nolint:paralleltest // Uses global module state.
	_, sluiceAddr := startTestSluice(t, "udp4")

	app, err := net.DialUDP("udp4", nil, sluiceAddr.(*net.UDPAddr)) //nolint:forcetypeassert
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	setDeadline(t, app)

	_, err = app.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, err := app.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], entrypointInfoMsg) {
		t.Fatalf("unexpected reply: %q", buf[:n])
	}
}

func TestTCPSluiceWithoutRequest(t *testing.T) { //nolint:paralleltest // Uses global module state.
	_, sluiceAddr := startTestSluice(t, "tcp4")

	app, err := net.Dial("tcp4", sluiceAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	setDeadline(t, app)

	reply, err := io.ReadAll(app)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, entrypointInfoMsg) {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestSluiceAbandonNotifiesPendingRequests(t *testing.T) { //nolint:paralleltest // Uses global module state.
	s, _ := startTestSluice(t, "tcp4")

	notified := make(chan net.Conn, 1)
	err := AwaitRequest(
		testConnInfo(packet.TCP, net.IPv4(127, 0, 0, 1), 1),
		func(_ *network.Connection, conn net.Conn) { notified <- conn },
	)
	if err != nil {
		t.Fatal(err)
	}

	s.abandon()
	select {
	case conn := <-notified:
		if conn != nil {
			t.Fatal("expected nil conn on abandon")
		}
	default:
		t.Fatal("pending request was not notified")
	}
	_, ok := getSluice("tcp4")
	if ok {
		t.Fatal("abandoned sluice is still registered")
	}
}

func TestUDPListenerCleanInactiveConns(t *testing.T) {
	t.Parallel()

	ln := &UDPListener{
		closed:   abool.New(),
		closing:  make(chan struct{}),
		newConns: make(chan *UDPConn),
		conns:    make(map[string]*UDPConn),
	}
	conn := &UDPConn{
		ln:            ln,
		addr:          &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
		closed:        abool.New(),
		closing:       make(chan struct{}),
		in:            make(chan []byte, 1),
		inactivityCnt: new(uint32),
	}
	ln.setConn(conn)

	for range 10 {
		ln.cleanInactiveConns(10)
	}
	if conn.closed.IsSet() {
		t.Fatal("conn closed too early")
	}
	ln.cleanInactiveConns(10)
	if !conn.closed.IsSet() {
		t.Fatal("inactive conn not closed")
	}
	for range 10 {
		ln.cleanInactiveConns(10)
	}
	if _, ok := ln.getConn(conn.addr.String()); ok {
		t.Fatal("inactive conn not removed")
	}
}
