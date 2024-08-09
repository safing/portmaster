package sluice

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/mgr"
)

// PacketListener is a listener for packet based protocols.
type PacketListener struct {
	sock     net.PacketConn
	closed   *abool.AtomicBool
	newConns chan *PacketConn

	lock  sync.Mutex
	conns map[string]*PacketConn
	err   error
}

// ListenPacket creates a packet listener.
func ListenPacket(network, address string) (net.Listener, error) {
	// Create a new listening packet socket.
	sock, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}

	// Create listener and start workers.
	ln := &PacketListener{
		sock:     sock,
		closed:   abool.New(),
		newConns: make(chan *PacketConn),
		conns:    make(map[string]*PacketConn),
	}
	module.mgr.Go("packet listener reader", ln.reader)
	module.mgr.Go("packet listener cleaner", ln.cleaner)

	return ln, nil
}

// Accept waits for and returns the next connection to the listener.
func (ln *PacketListener) Accept() (net.Conn, error) {
	conn := <-ln.newConns
	if conn != nil {
		return conn, nil
	}

	// Check if there is a socket error.
	ln.lock.Lock()
	defer ln.lock.Unlock()
	if ln.err != nil {
		return nil, ln.err
	}

	return nil, io.EOF
}

// Close closes the listener.
// Any blocked Accept operations will be unblocked and return errors.
func (ln *PacketListener) Close() error {
	if !ln.closed.SetToIf(false, true) {
		return nil
	}

	// Close all channels.
	close(ln.newConns)
	ln.lock.Lock()
	defer ln.lock.Unlock()
	for _, conn := range ln.conns {
		close(conn.in)
	}

	// Close socket.
	return ln.sock.Close()
}

// Addr returns the listener's network address.
func (ln *PacketListener) Addr() net.Addr {
	return ln.sock.LocalAddr()
}

func (ln *PacketListener) getConn(remoteAddr string) (conn *PacketConn, ok bool) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	conn, ok = ln.conns[remoteAddr]
	return
}

func (ln *PacketListener) setConn(conn *PacketConn) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	ln.conns[conn.addr.String()] = conn
}

func (ln *PacketListener) reader(_ *mgr.WorkerCtx) error {
	for {
		// Read data from connection.
		buf := make([]byte, 512)
		n, addr, err := ln.sock.ReadFrom(buf)
		if err != nil {
			// Set socket error.
			ln.lock.Lock()
			ln.err = err
			ln.lock.Unlock()
			// Close and return
			_ = ln.Close()
			return nil //nolint:nilerr
		}
		buf = buf[:n]

		// Get connection and supply data.
		conn, ok := ln.getConn(addr.String())
		if ok {
			// Ignore if conn is closed.
			if conn.closed.IsSet() {
				continue
			}

			select {
			case conn.in <- buf:
			default:
			}
			continue
		}

		// Or create a new connection.
		conn = &PacketConn{
			ln:            ln,
			addr:          addr,
			closed:        abool.New(),
			closing:       make(chan struct{}),
			buf:           buf,
			in:            make(chan []byte, 1),
			inactivityCnt: new(uint32),
		}
		ln.setConn(conn)
		ln.newConns <- conn
	}
}

func (ln *PacketListener) cleaner(ctx *mgr.WorkerCtx) error {
	for {
		select {
		case <-time.After(1 * time.Minute):
			// Check if listener has died.
			if ln.closed.IsSet() {
				return nil
			}
			// Clean connections.
			ln.cleanInactiveConns(10)

		case <-ctx.Done():
			// Exit with module stop.
			return nil
		}
	}
}

func (ln *PacketListener) cleanInactiveConns(overInactivityCnt uint32) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	for k, conn := range ln.conns {
		cnt := atomic.AddUint32(conn.inactivityCnt, 1)
		switch {
		case cnt > overInactivityCnt*2:
			delete(ln.conns, k)
		case cnt > overInactivityCnt:
			_ = conn.Close()
		}
	}
}

// PacketConn simulates a connection for a stateless protocol.
type PacketConn struct {
	ln      *PacketListener
	addr    net.Addr
	closed  *abool.AtomicBool
	closing chan struct{}

	buf []byte
	in  chan []byte

	inactivityCnt *uint32
}

// Read reads data from the connection.
// Read can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetReadDeadline.
func (conn *PacketConn) Read(b []byte) (n int, err error) {
	// Check if connection is closed.
	if conn.closed.IsSet() {
		return 0, io.EOF
	}

	// Mark as active.
	atomic.StoreUint32(conn.inactivityCnt, 0)

	// Get new buffer.
	if conn.buf == nil {
		select {
		case conn.buf = <-conn.in:
			if conn.buf == nil {
				return 0, io.EOF
			}
		case <-conn.closing:
			return 0, io.EOF
		}
	}

	// Serve from buffer.
	copy(b, conn.buf)
	if len(b) >= len(conn.buf) {
		copied := len(conn.buf)
		conn.buf = nil
		return copied, nil
	}
	copied := len(b)
	conn.buf = conn.buf[copied:]
	return copied, nil
}

// Write writes data to the connection.
// Write can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetWriteDeadline.
func (conn *PacketConn) Write(b []byte) (n int, err error) {
	// Check if connection is closed.
	if conn.closed.IsSet() {
		return 0, io.EOF
	}

	// Mark as active.
	atomic.StoreUint32(conn.inactivityCnt, 0)

	return conn.ln.sock.WriteTo(b, conn.addr)
}

// Close is a no-op as UDP connections share a single socket. Just stop sending
// packets without closing.
func (conn *PacketConn) Close() error {
	if conn.closed.SetToIf(false, true) {
		close(conn.closing)
	}
	return nil
}

// LocalAddr returns the local network address.
func (conn *PacketConn) LocalAddr() net.Addr {
	return conn.ln.sock.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (conn *PacketConn) RemoteAddr() net.Addr {
	return conn.addr
}

// SetDeadline is a no-op as UDP connections share a single socket.
func (conn *PacketConn) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline is a no-op as UDP connections share a single socket.
func (conn *PacketConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline is a no-op as UDP connections share a single socket.
func (conn *PacketConn) SetWriteDeadline(t time.Time) error {
	return nil
}
