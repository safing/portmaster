package proxy

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// udpSession is the NAT entry for a single client endpoint.
type udpSession struct {
	connCtx *ConnContext
	// remote is the per-session UDP socket dialled to the upstream.
	remote *net.UDPConn
}

// UDPProxy is a Layer-4 UDP proxy.  It uses a single listening UDPConn and
// maintains a NAT-like table that maps each client address to a dedicated
// upstream socket.  Sessions are evicted after Config.ReadTimeout of inactivity
// (default 5 min).
type UDPProxy struct {
	decider DeciderFunc
	log     Logger
	cfg     Config

	conn  *net.UDPConn // listener
	cache *sessionCache

	// sessions maps clientAddr.String() → *udpSession.
	mu       sync.RWMutex
	sessions map[string]*udpSession

	shutdownCtx context.Context
	shutdown    context.CancelFunc

	once sync.Once
	wg   sync.WaitGroup
}

// udpBufPool holds reusable 64 KiB byte slices for UDP datagram I/O.
// 64 KiB is the maximum size of a UDP payload, so this size avoids fragmentation
// for any datagram.  The pool amortizes the cost of allocating these buffers,
// which are large enough to trigger GC pressure if allocated on every packet.
var udpBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 64*1024)
		return &b
	},
}

// NewUDPProxy creates and starts a UDP proxy listening on listenAddr.
// It uses DefaultConfig for tuning parameters.
func NewUDPProxy(listenAddr string, decider DeciderFunc, logger Logger) (*UDPProxy, error) {
	return NewUDPProxyWithConfig(listenAddr, decider, logger, DefaultConfig())
}

// NewUDPProxyWithConfig is like NewUDPProxy but accepts a custom Config.
func NewUDPProxyWithConfig(listenAddr string, decider DeciderFunc, logger Logger, cfg Config) (*UDPProxy, error) {
	if decider == nil {
		return nil, errors.New("proxy: decider must not be nil")
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = DEFAULT_READ_TIMEOUT
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = DEFAULT_WRITE_TIMEOUT
	}

	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &UDPProxy{
		decider:     decider,
		log:         resolveLogger(logger),
		cfg:         cfg,
		conn:        conn,
		cache:       newSessionCache(),
		sessions:    make(map[string]*udpSession, 64),
		shutdownCtx: ctx,
		shutdown:    cancel,
	}

	p.wg.Add(2)
	go p.readLoop()
	go p.cleanupLoop()

	p.log.Infof("udp proxy: listening on %s", conn.LocalAddr())
	return p, nil
}

// Addr returns the address the proxy is listening on.
func (p *UDPProxy) Addr() net.Addr {
	return p.conn.LocalAddr()
}

// Metrics returns a snapshot of the session cache statistics.
func (p *UDPProxy) Metrics() Metrics {
	return p.cache.metrics()
}

// Shutdown tears down the proxy.  It closes the listen socket, cancels all
// sessions, and waits for goroutines to exit or until ctx expires.
func (p *UDPProxy) Shutdown(ctx context.Context) error {
	var retErr error
	p.once.Do(func() {
		p.log.Infof("udp proxy: shutting down (%v)", p.cache.metrics())

		// Signal all goroutines and unblock ReadFromUDP.
		p.shutdown()
		p.conn.Close()

		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			p.log.Infof("udp proxy: shutdown complete")
		case <-ctx.Done():
			retErr = ctx.Err()
			p.log.Warnf("udp proxy: forced shutdown: %v", retErr)
		}
	})
	return retErr
}

// ─── Inbound read loop ────────────────────────────────────────────────────────

func (p *UDPProxy) readLoop() {
	defer p.wg.Done()
	for {
		bp := udpBufPool.Get().(*[]byte)
		n, clientAddr, err := p.conn.ReadFromUDP(*bp)
		if err != nil {
			udpBufPool.Put(bp)
			select {
			case <-p.shutdownCtx.Done():
				return
			default:
				p.log.Errorf("udp proxy: read: %v", err)
				return
			}
		}

		// Copy payload so we can return the pooled buffer immediately.
		data := make([]byte, n)
		copy(data, (*bp)[:n])
		udpBufPool.Put(bp)

		p.handlePacket(clientAddr, data)
	}
}

// handlePacket routes one inbound datagram to the correct upstream session.
func (p *UDPProxy) handlePacket(clientAddr *net.UDPAddr, data []byte) {
	key := clientAddr.String()

	// Fast path: session already exists.
	p.mu.RLock()
	sess, ok := p.sessions[key]
	p.mu.RUnlock()

	if ok {
		sess.connCtx.touch()
		sess.connCtx.PacketsOut.Add(1)
		sess.connCtx.BytesOut.Add(uint64(len(data)))

		_ = sess.remote.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
		if _, err := sess.remote.Write(data); err != nil {
			if !isClosedConnErr(err) {
				p.log.Warnf("udp proxy: write to upstream for %s: %v", key, err)
			}
		}
		return
	}

	// Slow path: new client — enforce session limit before allocating.
	if p.cfg.MaxSessions > 0 && p.cache.len() >= p.cfg.MaxSessions {
		p.log.Warnf("udp proxy: max sessions (%d) reached, dropping packet from %s",
			p.cfg.MaxSessions, key)
		return
	}

	dest, localAddr, err := p.decider(p.conn.LocalAddr(), clientAddr)
	if err != nil {
		p.log.Warnf("udp proxy: decider rejected %s: %v", key, err)
		return
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		p.log.Errorf("udp proxy: resolve %q: %v", dest, err)
		return
	}
	var localUDPAddr *net.UDPAddr
	if localAddr != "" {
		localUDPAddr, err = net.ResolveUDPAddr("udp", localAddr)
		if err != nil {
			p.log.Errorf("udp proxy: resolve local addr %q: %v", localAddr, err)
			return
		}
	}
	remoteConn, err := net.DialUDP("udp", localUDPAddr, remoteAddr)
	if err != nil {
		p.log.Errorf("udp proxy: dial %q: %v", dest, err)
		return
	}

	sessCtx, cancel := context.WithCancel(p.shutdownCtx)
	connCtx := newConnContext(nextID(), clientAddr, dest, cancel)
	sess = &udpSession{connCtx: connCtx, remote: remoteConn}

	// Write-lock: check again to prevent duplicate sessions under contention.
	p.mu.Lock()
	if existing, exists := p.sessions[key]; exists {
		p.mu.Unlock()
		cancel()
		remoteConn.Close()

		// Reuse the existing session for this datagram.
		existing.connCtx.touch()
		existing.connCtx.PacketsOut.Add(1)
		existing.connCtx.BytesOut.Add(uint64(len(data)))

		_ = existing.remote.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
		if _, err := existing.remote.Write(data); err != nil {
			p.log.Warnf("udp proxy: write to upstream for %s: %v", key, err)
		}
		return
	}
	p.sessions[key] = sess
	p.mu.Unlock()

	p.cache.add(connCtx)
	p.log.Debugf("udp proxy: session %d  %s → %s", connCtx.ID, key, dest)

	// Launch reverse-direction goroutine (upstream → client).
	p.wg.Add(1)
	go p.forwardFromRemote(sessCtx, sess, clientAddr)

	// Forward the first datagram.
	connCtx.touch()
	connCtx.PacketsOut.Add(1)
	connCtx.BytesOut.Add(uint64(len(data)))

	_ = remoteConn.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
	if _, err := remoteConn.Write(data); err != nil {
		p.log.Warnf("udp proxy: initial write to upstream: %v", err)
	}
}

// ─── Upstream → client forwarder ─────────────────────────────────────────────

// forwardFromRemote reads replies from the upstream socket and writes them
// back to the originating client.  It exits when the context is cancelled,
// an idle timeout fires, or an unrecoverable read/write error occurs.
func (p *UDPProxy) forwardFromRemote(ctx context.Context, sess *udpSession, clientAddr *net.UDPAddr) {
	defer p.wg.Done()
	defer p.removeSession(sess, clientAddr.String())

	for {
		// Check for cancellation before each read.
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Roll the read deadline before every read so a truly silent upstream
		// is detected within ReadTimeout.
		_ = sess.remote.SetReadDeadline(time.Now().Add(p.cfg.ReadTimeout))

		bp := udpBufPool.Get().(*[]byte)
		n, err := sess.remote.Read(*bp)
		if err != nil {
			udpBufPool.Put(bp)
			select {
			case <-ctx.Done():
				return
			default:
				if isTimeoutErr(err) {
					p.log.Debugf("udp proxy: session %d idle timeout (%s)", sess.connCtx.ID, clientAddr)
					return
				}
				if !isClosedConnErr(err) {
					p.log.Debugf("udp proxy: session %d read error: %v", sess.connCtx.ID, err)
				}
				return
			}
		}

		// Write to listen socket and return buffer.  WriteToUDP is safe to
		// call concurrently on the same *net.UDPConn; each goroutine resets
		// the write deadline immediately before its own write, so concurrent
		// sessions may shift each other's deadline by at most WriteTimeout.
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
		_, writeErr := p.conn.WriteToUDP((*bp)[:n], clientAddr)
		udpBufPool.Put(bp)

		if writeErr != nil {
			select {
			case <-ctx.Done():
				return
			default:
				if !isClosedConnErr(writeErr) {
					p.log.Warnf("udp proxy: write to client %s: %v", clientAddr, writeErr)
				}
				return
			}
		}

		sess.connCtx.touch()
		sess.connCtx.PacketsIn.Add(1)
		sess.connCtx.BytesIn.Add(uint64(n))
	}
}

// removeSession evicts sess from the NAT table and the session cache.
func (p *UDPProxy) removeSession(sess *udpSession, key string) {
	sess.remote.Close()
	sess.connCtx.Close()

	p.mu.Lock()
	delete(p.sessions, key)
	p.mu.Unlock()

	p.cache.remove(sess.connCtx.ID)
	p.log.Debugf("udp proxy: session %d removed — in=%d out=%d",
		sess.connCtx.ID, sess.connCtx.BytesIn.Load(), sess.connCtx.BytesOut.Load())
}

// ─── Idle cleanup loop ────────────────────────────────────────────────────────

// cleanupLoop periodically inspects the NAT table and cancels sessions whose
// last-seen time predates the idle timeout.
func (p *UDPProxy) cleanupLoop() {
	defer p.wg.Done()

	interval := p.cfg.ReadTimeout / 2
	if interval < time.Second*10 {
		interval = time.Second * 10
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.shutdownCtx.Done():
			// Cancel all sessions and close their remote sockets so that
			// forwardFromRemote goroutines unblock from Read immediately.
			p.mu.Lock()
			for _, sess := range p.sessions {
				sess.connCtx.Close()
				sess.remote.Close() // unblocks pending Read in forwardFromRemote
			}
			p.mu.Unlock()
			return
		case <-ticker.C:
			p.evictIdle()
		}
	}
}

// evictIdle cancels sessions that have been idle longer than ReadTimeout.
// It closes the remote socket so forwardFromRemote's Read unblocks faster
// than waiting for the next rolling deadline.
func (p *UDPProxy) evictIdle() {
	threshold := time.Now().Add(-p.cfg.ReadTimeout)

	p.mu.RLock()
	defer p.mu.RUnlock()

	for key, sess := range p.sessions {
		if sess.connCtx.LastSeen().Before(threshold) {
			p.log.Debugf("udp proxy: evicting idle session %d (%s)", sess.connCtx.ID, key)
			sess.connCtx.Close()
			// Wake the blocked Read so the goroutine notices ctx.Done().
			_ = sess.remote.SetReadDeadline(time.Now())
		}
	}
}

// ─── Error helpers ────────────────────────────────────────────────────────────

// isTimeoutErr returns true if err is a network timeout error.
func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
