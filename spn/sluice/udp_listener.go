package sluice

import (
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/safing/portmaster/service/mgr"
)

const onWindows = runtime.GOOS == "windows"

// UDPListener is a listener for UDP.
type UDPListener struct {
	sock     *net.UDPConn
	closed   *abool.AtomicBool
	newConns chan *UDPConn
	oobSize  int

	lock  sync.Mutex
	conns map[string]*UDPConn
	err   error
}

// ListenUDP creates a packet listener.
func ListenUDP(network, address string) (net.Listener, error) {
	// Parse address.
	udpAddr, err := net.ResolveUDPAddr(network, address)
	if err != nil {
		return nil, err
	}

	// Determine oob data size.
	oobSize := 40 // IPv6 (measured)
	if udpAddr.IP.To4() != nil {
		oobSize = 32 // IPv4 (measured)
	}

	// Create a new listening UDP socket.
	sock, err := net.ListenUDP(network, udpAddr)
	if err != nil {
		return nil, err
	}

	// Create listener.
	ln := &UDPListener{
		sock:     sock,
		closed:   abool.New(),
		newConns: make(chan *UDPConn),
		oobSize:  oobSize,
		conns:    make(map[string]*UDPConn),
	}

	// Set socket options on listener.
	err = ln.setSocketOptions()
	if err != nil {
		return nil, err
	}

	// Start workers.
	module.mgr.Go("udp listener reader", ln.reader)
	module.mgr.Go("udp listener cleaner", ln.cleaner)

	return ln, nil
}

// Accept waits for and returns the next connection to the listener.
func (ln *UDPListener) Accept() (net.Conn, error) {
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
func (ln *UDPListener) Close() error {
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
func (ln *UDPListener) Addr() net.Addr {
	return ln.sock.LocalAddr()
}

func (ln *UDPListener) getConn(remoteAddr string) (conn *UDPConn, ok bool) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	conn, ok = ln.conns[remoteAddr]
	return
}

func (ln *UDPListener) setConn(conn *UDPConn) {
	ln.lock.Lock()
	defer ln.lock.Unlock()

	ln.conns[conn.addr.String()] = conn
}

func (ln *UDPListener) reader(_ *mgr.WorkerCtx) error {
	for {
		// TODO: Find good buf size.
		// With a buf size of 512 we have seen this error on Windows:
		// wsarecvmsg: A message sent on a datagram socket was larger than the internal message buffer or some other network limit, or the buffer used to receive a datagram into was smaller than the datagram itself.
		// UDP is not (yet) heavily used, so we can go for the 1500 bytes size for now.

		// Read data from connection.
		buf := make([]byte, 1500) // TODO: see comment above.
		oob := make([]byte, ln.oobSize)
		n, oobn, _, addr, err := ln.sock.ReadMsgUDP(buf, oob)
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
		oob = oob[:oobn]

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
		conn = &UDPConn{
			ln:            ln,
			addr:          addr,
			oob:           oob,
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

func (ln *UDPListener) cleaner(ctx *mgr.WorkerCtx) error {
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

func (ln *UDPListener) cleanInactiveConns(overInactivityCnt uint32) {
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

// setUDPSocketOptions sets socket options so that the source address for
// replies is correct.
func (ln *UDPListener) setSocketOptions() error {
	// Setting socket options is not supported on windows.
	if onWindows {
		return nil
	}

	// As we might be listening on an interface that supports both IPv4 and IPv6,
	// try to set the socket options on both.
	// Only report an error if it fails on both.
	err4 := ipv4.NewPacketConn(ln.sock).SetControlMessage(ipv4.FlagDst|ipv4.FlagInterface, true)
	err6 := ipv6.NewPacketConn(ln.sock).SetControlMessage(ipv6.FlagDst|ipv6.FlagInterface, true)
	if err4 != nil && err6 != nil {
		return err4
	}

	return nil
}

// UDPConn simulates a connection for a stateless protocol.
type UDPConn struct {
	ln      *UDPListener
	addr    *net.UDPAddr
	oob     []byte
	closed  *abool.AtomicBool
	closing chan struct{}

	buf []byte
	in  chan []byte

	inactivityCnt *uint32
}

// Read reads data from the connection.
// Read can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetReadDeadline.
func (conn *UDPConn) Read(b []byte) (n int, err error) {
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
func (conn *UDPConn) Write(b []byte) (n int, err error) {
	// Check if connection is closed.
	if conn.closed.IsSet() {
		return 0, io.EOF
	}

	// Mark as active.
	atomic.StoreUint32(conn.inactivityCnt, 0)

	n, _, err = conn.ln.sock.WriteMsgUDP(b, conn.oob, conn.addr)
	return n, err
}

// Close is a no-op as UDP connections share a single socket. Just stop sending
// packets without closing.
func (conn *UDPConn) Close() error {
	if conn.closed.SetToIf(false, true) {
		close(conn.closing)
	}
	return nil
}

// LocalAddr returns the local network address.
func (conn *UDPConn) LocalAddr() net.Addr {
	return conn.ln.sock.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (conn *UDPConn) RemoteAddr() net.Addr {
	return conn.addr
}

// SetDeadline is a no-op as UDP connections share a single socket.
func (conn *UDPConn) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline is a no-op as UDP connections share a single socket.
func (conn *UDPConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline is a no-op as UDP connections share a single socket.
func (conn *UDPConn) SetWriteDeadline(t time.Time) error {
	return nil
}
