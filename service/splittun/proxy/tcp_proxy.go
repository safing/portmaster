package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// TCPProxy is a Layer-4 TCP proxy that routes each accepted connection through
// a DeciderFunc before dialling the upstream destination.
//
// It is safe to call Shutdown from any goroutine and from multiple goroutines
// simultaneously (only the first call has effect).
type TCPProxy struct {
	decider DeciderFunc
	log     Logger
	cfg     Config

	listener    net.Listener
	bufPool     sync.Pool
	cache       *sessionCache
	shutdownCtx context.Context
	shutdown    context.CancelFunc

	once sync.Once
	wg   sync.WaitGroup
}

// NewTCPProxy creates and starts a TCP proxy that listens on listenAddr.
// It uses DefaultConfig for tuning parameters.
//
// The proxy begins accepting connections immediately; call Shutdown to stop it.
func NewTCPProxy(listenAddr string, decider DeciderFunc, logger Logger) (*TCPProxy, error) {
	return NewTCPProxyWithConfig(listenAddr, decider, logger, DefaultConfig())
}

// NewTCPProxyWithConfig is like NewTCPProxy but accepts a custom Config.
func NewTCPProxyWithConfig(listenAddr string, decider DeciderFunc, logger Logger, cfg Config) (*TCPProxy, error) {
	if decider == nil {
		return nil, errors.New("proxy: decider must not be nil")
	}

	if cfg.BufferSize <= 0 {
		cfg.BufferSize = DEFAULT_BUFFER_SIZE
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = DEFAULT_DIAL_TIMEOUT
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = DEFAULT_READ_TIMEOUT
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = DEFAULT_WRITE_TIMEOUT
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &TCPProxy{
		decider:     decider,
		log:         resolveLogger(logger),
		cfg:         cfg,
		listener:    ln,
		cache:       newSessionCache(),
		shutdownCtx: ctx,
		shutdown:    cancel,
	}
	bufSize := cfg.BufferSize
	p.bufPool.New = func() interface{} {
		b := make([]byte, bufSize)
		return &b
	}

	p.wg.Add(1)
	go p.acceptLoop()

	p.log.Infof("tcp proxy: listening on %s", ln.Addr())
	return p, nil
}

// Addr returns the address the proxy is listening on.
func (p *TCPProxy) Addr() net.Addr {
	return p.listener.Addr()
}

// Metrics returns a snapshot of the session cache statistics.
func (p *TCPProxy) Metrics() Metrics {
	return p.cache.metrics()
}

// Shutdown stops accepting new connections, signals all active sessions to
// close, and waits for them to finish.  If ctx expires before all sessions
// drain, it returns ctx.Err() but does not leak goroutines — connections are
// already being forcibly closed by the context cancellation.
func (p *TCPProxy) Shutdown(ctx context.Context) error {
	var retErr error
	p.once.Do(func() {
		p.log.Infof("tcp proxy: shutting down (%v)", p.cache.metrics())

		// Unblock Accept and signal all per-session goroutines.
		p.listener.Close()
		p.shutdown()

		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			p.log.Infof("tcp proxy: shutdown complete (%v)", p.cache.metrics())
		case <-ctx.Done():
			retErr = ctx.Err()
			p.log.Warnf("tcp proxy: forced shutdown: %v", retErr)
		}
	})
	return retErr
}

// ─── accept loop ──────────────────────────────────────────────────────────────

func (p *TCPProxy) acceptLoop() {
	defer p.wg.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.shutdownCtx.Done():
				return
			default:
				// Transient OS error (e.g. EMFILE).  Back off briefly.
				p.log.Errorf("tcp proxy: accept: %v", err)
				time.Sleep(5 * time.Millisecond)
				continue
			}
		}

		if p.cfg.MaxSessions > 0 && p.cache.len() >= p.cfg.MaxSessions {
			p.log.Warnf("tcp proxy: max sessions (%d) reached, rejecting %s",
				p.cfg.MaxSessions, conn.RemoteAddr())
			conn.Close()
			continue
		}

		p.wg.Add(1)
		go p.handleConn(conn)
	}
}

// ─── per-connection handler ───────────────────────────────────────────────────

func (p *TCPProxy) handleConn(clientConn net.Conn) {
	defer p.wg.Done()
	defer clientConn.Close()

	// Determine upstream destination.
	dest, localAddr, err := p.decider(p.listener.Addr(), clientConn.RemoteAddr())
	if err != nil {
		p.log.Warnf("tcp proxy: decider rejected %s: %v", clientConn.RemoteAddr(), err)
		return
	}

	// DialContext is cancelled immediately if the proxy is shut down.
	dialer := net.Dialer{Timeout: p.cfg.DialTimeout}
	if localAddr != "" {
		tcpAddr, resolveErr := net.ResolveTCPAddr("tcp", localAddr)
		if resolveErr != nil {
			p.log.Errorf("tcp proxy: resolve local addr %q: %v", localAddr, resolveErr)
			return
		}
		dialer.LocalAddr = tcpAddr
	}
	upstreamConn, err := dialer.DialContext(p.shutdownCtx, "tcp", dest)
	if err != nil {
		if p.shutdownCtx.Err() != nil {
			// Proxy is shutting down; this is expected, not an error.
			return
		}
		p.log.Errorf("tcp proxy: dial %q: %v", dest, err)
		return
	}
	defer upstreamConn.Close()

	// Register the session.
	sessCtx, cancel := context.WithCancel(p.shutdownCtx)
	connCtx := newConnContext(
		nextID(),
		clientConn.RemoteAddr(),
		dest,
		cancel,
	)
	p.cache.add(connCtx)

	defer func() {
		cancel()
		p.cache.remove(connCtx.ID)
		p.log.Debugf("tcp proxy: session %d closed — in=%d out=%d",
			connCtx.ID, connCtx.BytesIn.Load(), connCtx.BytesOut.Load())
	}()

	p.log.Debugf("tcp proxy: session %d  %s → %s", connCtx.ID,
		clientConn.RemoteAddr(), dest)

	// Watchdog: when the proxy shuts down (or the caller cancels the session),
	// force-close both ends so the copy goroutines unblock immediately.
	go func() {
		<-sessCtx.Done()
		clientConn.Close()
		upstreamConn.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// client → upstream
	go func() {
		defer wg.Done()
		n := p.pipe(upstreamConn, clientConn, connCtx)
		connCtx.BytesOut.Add(uint64(n))
		// Propagate clean EOF downstream.
		halfClose(upstreamConn)
	}()

	// upstream → client
	go func() {
		defer wg.Done()
		n := p.pipe(clientConn, upstreamConn, connCtx)
		connCtx.BytesIn.Add(uint64(n))
		halfClose(clientConn)
	}()

	wg.Wait()
}

// pipe copies from src to dst using a manual read/write loop with a pooled
// buffer and returns the total bytes transferred.
//
// io.CopyBuffer is not used because it provides no opportunity
// to call SetReadDeadline/SetWriteDeadline between individual I/O operations.
func (p *TCPProxy) pipe(dst, src net.Conn, connCtx *ConnContext) int64 {
	bp := p.bufPool.Get().(*[]byte)
	defer p.bufPool.Put(bp)
	buf := *bp

	var total int64
	for {
		_ = src.SetReadDeadline(time.Now().Add(p.cfg.ReadTimeout))
		nr, readErr := src.Read(buf)

		if nr > 0 {
			connCtx.touch() // session is active; reset idle tracking
			_ = dst.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
			nw, writeErr := dst.Write(buf[:nr])
			total += int64(nw)
			if writeErr != nil {
				if errors.Is(writeErr, os.ErrDeadlineExceeded) {
					p.log.Debugf("tcp proxy: session %d write timeout (%v)", connCtx.ID, p.cfg.WriteTimeout)
				} else if !isClosedConnErr(writeErr) {
					p.log.Debugf("tcp proxy: session %d write error: %v", connCtx.ID, writeErr)
				}
				break
			}
		}
		if readErr != nil {
			if errors.Is(readErr, os.ErrDeadlineExceeded) {
				p.log.Debugf("tcp proxy: session %d read timeout (%v)", connCtx.ID, p.cfg.ReadTimeout)
			} else if !isClosedConnErr(readErr) {
				p.log.Debugf("tcp proxy: session %d read error: %v", connCtx.ID, readErr)
			}
			break
		}
	}
	return total
}

// halfClose attempts a TCP write-shutdown so the peer receives EOF on its
// read side while the connection stays open for the other direction.
func halfClose(conn net.Conn) {
	type canCloseWrite interface{ CloseWrite() error }
	if c, ok := conn.(canCloseWrite); ok {
		_ = c.CloseWrite()
	}
}

// isClosedConnErr returns true for errors that arise from an already-closed
// connection or a clean EOF — these are expected during shutdown.
func isClosedConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		// "use of closed network connection" and "connection reset by peer"
		// are both acceptable on shutdown.
		return true
	}
	return false
}
