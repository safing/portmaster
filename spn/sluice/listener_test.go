package sluice

import (
	"net"
	"testing"
	"time"

	"github.com/tevino/abool"
)

// runCatchingPanic runs fn and returns the recovered panic value, if any.
func runCatchingPanic(fn func()) (recovered any) {
	defer func() {
		recovered = recover()
	}()
	fn()
	return nil
}

// waitFor polls cond until it returns true or the timeout is reached.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

// testableListener abstracts the UDP and the packet listener for tests.
type testableListener interface {
	net.Listener
	hasConn(remoteAddr string) bool
	runReader()
}

func (ln *UDPListener) hasConn(remoteAddr string) bool {
	_, ok := ln.getConn(remoteAddr)
	return ok
}
func (ln *UDPListener) runReader() { _ = ln.reader(nil) }

func (ln *PacketListener) hasConn(remoteAddr string) bool {
	_, ok := ln.getConn(remoteAddr)
	return ok
}
func (ln *PacketListener) runReader() { _ = ln.reader(nil) }

// testCloseDuringHandover closes the listener while the reader is handing a
// new connection to Accept, which must not panic.
func testCloseDuringHandover(t *testing.T, ln testableListener) {
	t.Helper()

	readerPanic := make(chan any, 1)
	go func() {
		readerPanic <- runCatchingPanic(ln.runReader)
	}()

	// Send a packet from a new peer, but do not accept it.
	peer, err := net.DialUDP("udp4", nil, ln.Addr().(*net.UDPAddr)) //nolint:forcetypeassert
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peer.Close() }()
	_, err = peer.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return ln.hasConn(peer.LocalAddr().String()) })

	// Close the listener while the reader is (about to be) blocked handing over.
	err = ln.Close()
	if err != nil {
		t.Fatal(err)
	}
	if p := <-readerPanic; p != nil {
		t.Fatalf("reader panicked: %v", p)
	}
	_, err = ln.Accept()
	if err == nil {
		t.Fatal("expected error from Accept on closed listener")
	}
}

func TestUDPListenerCloseDuringHandover(t *testing.T) {
	t.Parallel()

	sock, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	testCloseDuringHandover(t, &UDPListener{
		sock:     sock,
		closed:   abool.New(),
		closing:  make(chan struct{}),
		newConns: make(chan *UDPConn),
		oobSize:  32,
		conns:    make(map[string]*UDPConn),
	})
}

func TestPacketListenerCloseDuringHandover(t *testing.T) {
	t.Parallel()

	sock, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	testCloseDuringHandover(t, &PacketListener{
		sock:     sock,
		closed:   abool.New(),
		closing:  make(chan struct{}),
		newConns: make(chan *PacketConn),
		conns:    make(map[string]*PacketConn),
	})
}
