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
	decider   DeciderFunc
	log       Logger
	cfg       Config
	network   string
	logPrefix string

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
//
// Parameters:
//   - listenAddr: the local address to listen on (e.g. "0.0.0.0:719")
//   - network: the network type to listen on (e.g. "tcp4", "tcp6", "udp4", "udp6")
//   - decider: a function that determines the upstream destination for each
//     accepted connection.  See DeciderFunc for details.
//   - logger: an optional Logger for debug/info/warn messages.  If nil, a
//     default logger is used.
//   - logPrefix: a string prepended to every log message (e.g. "tcp proxy IPv4").
//     Pass an empty string to log messages without a prefix.
func NewTCPProxy(listenAddr string, network string, decider DeciderFunc, logger Logger, logPrefix string) (*TCPProxy, error) {
	return NewTCPProxyWithConfig(listenAddr, network, decider, logger, DefaultConfig(), logPrefix)
}

// NewTCPProxyWithConfig is like NewTCPProxy but accepts a custom Config.
func NewTCPProxyWithConfig(listenAddr string, network string, decider DeciderFunc, logger Logger, cfg Config, logPrefix string) (*TCPProxy, error) {
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

	ln, err := net.Listen(network, listenAddr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &TCPProxy{
		decider:     decider,
		log:         resolveLogger(logger),
		cfg:         cfg,
		network:     network,
		logPrefix:   resolveLogPrefix(logPrefix),
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

	p.log.Debug(p.logPrefix+"listening", "addr", ln.Addr())
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

// FindProxiedEgressConnection returns all active (including establishing)
// sessions whose upstream destination matches destIP and destPort.
// Returns nil if no matching session exists.
func (p *TCPProxy) FindProxiedEgressConnection(destIP net.IP, destPort uint16) []*ConnContext {
	return p.cache.findByDest(destIP, destPort)
}

// HasProxiedEgressConnection checks if there is an active session whose
// upstream destination matches destIP and destPort.
func (p *TCPProxy) HasProxiedEgressConnection(destIP net.IP, destPort uint16) bool {
	return p.cache.hasByDest(destIP, destPort)
}

// Shutdown stops accepting new connections, signals all active sessions to
// close, and waits for them to finish.  If ctx expires before all sessions
// drain, it returns ctx.Err() but does not leak goroutines — connections are
// already being forcibly closed by the context cancellation.
func (p *TCPProxy) Shutdown(ctx context.Context) error {
	var retErr error
	p.once.Do(func() {
		p.shutdown()
		p.listener.Close()

		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			p.log.Debug(p.logPrefix+"shutdown complete", "metrics", p.cache.metrics())
		case <-ctx.Done():
			retErr = ctx.Err()
			p.log.Warn(p.logPrefix+"forced shutdown", "err", retErr)
		}
	})
	return retErr
}

// ─── accept loop ──────────────────────────────────────────────────────────────

func (p *TCPProxy) acceptLoop() {
	defer p.wg.Done()
	var backoff time.Duration
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.shutdownCtx.Done():
				return
			default:
				// Transient OS error (e.g. EMFILE).  Back off exponentially so
				// a sustained error produces at most ~1 log line per second.
				if backoff == 0 {
					backoff = 5 * time.Millisecond
				} else {
					backoff = min(backoff*2, time.Second)
				}
				p.log.Error(p.logPrefix+"accept error", "err", err)
				time.Sleep(backoff)
				continue
			}
		}
		backoff = 0 // reset on success

		if p.cfg.MaxSessions > 0 && p.cache.len() >= p.cfg.MaxSessions {
			p.log.Warn(p.logPrefix+"max sessions reached, rejecting connection", "max", p.cfg.MaxSessions, "addr", conn.RemoteAddr())
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
	destIP, destPort, binding, extraInfo, err := p.decider(p.listener.Addr(), clientConn.RemoteAddr())
	if err != nil {
		p.log.Warn(p.logPrefix+"decider rejected connection", "addr", clientConn.RemoteAddr(), "err", err)
		return
	}
	destAddr := (&net.TCPAddr{IP: destIP, Port: int(destPort)}).String()

	// Register the session immediately so FindProxiedEgressConnection can
	// locate it before the upstream dial completes.
	sessCtx, cancel := context.WithCancel(p.shutdownCtx)
	connCtx := newConnContext(
		nextID(),
		clientConn.RemoteAddr(),
		destIP,
		destPort,
		cancel,
		extraInfo,
	)
	p.cache.add(connCtx)

	defer func() {
		cancel()
		p.cache.remove(connCtx)
		p.log.Debug(p.logPrefix+"session closed", "session", connCtx.id, "dest_ip", connCtx.destIP, "dest_port", connCtx.destPort, "bytes_in", connCtx.BytesIn.Load(), "bytes_out", connCtx.BytesOut.Load())
	}()

	// DialContext is cancelled immediately if the proxy is shut down.
	dialer := net.Dialer{Timeout: p.cfg.DialTimeout}
	if binding != nil && binding.IP != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: binding.IP}
	}
	if binding != nil {
		applyBindToDevice(&dialer, binding.Interface)
	}
	upstreamConn, err := dialer.DialContext(p.shutdownCtx, p.network, destAddr)
	if err != nil {
		if p.shutdownCtx.Err() != nil {
			// Proxy is shutting down; this is expected, not an error.
			return
		}
		p.log.Error(p.logPrefix+"dial failed", "addr", destAddr, "err", err)
		return
	}
	defer upstreamConn.Close()

	p.log.Debug(p.logPrefix+"session started", "session", connCtx.id, "from", clientConn.RemoteAddr(), "to", destAddr)

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
					p.log.Debug(p.logPrefix+"session write timeout", "session", connCtx.id, "dest_ip", connCtx.destIP, "dest_port", connCtx.destPort, "timeout", p.cfg.WriteTimeout)
				} else if !isClosedConnErr(writeErr) {
					p.log.Debug(p.logPrefix+"session write error", "session", connCtx.id, "dest_ip", connCtx.destIP, "dest_port", connCtx.destPort, "err", writeErr)
				}
				break
			}
		}
		if readErr != nil {
			if errors.Is(readErr, os.ErrDeadlineExceeded) {
				p.log.Debug(p.logPrefix+"session read timeout", "session", connCtx.id, "dest_ip", connCtx.destIP, "dest_port", connCtx.destPort, "timeout", p.cfg.ReadTimeout)
			} else if !isClosedConnErr(readErr) {
				p.log.Debug(p.logPrefix+"session read error", "session", connCtx.id, "dest_ip", connCtx.destIP, "dest_port", connCtx.destPort, "err", readErr)
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

// isClosedConnErr reports whether err is a clean EOF or a closed-socket error
// that is expected during normal session teardown or proxy shutdown.
func isClosedConnErr(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}
