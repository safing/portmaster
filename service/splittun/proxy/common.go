// Package proxy provides minimal, Layer-4 TCP and UDP proxies
// with injected routing decisions (DeciderFunc), structured logging, session
// tracking, and graceful shutdown.
package proxy

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Public API types ────────────────────────────────────────────────────────

// DeciderFunc is called once per new session to determine the upstream
// destination address and the optional local address to bind the outgoing
// connection to.  local is the proxy's listen address; peer is the connecting
// client's address.  It returns a "host:port" dest, an optional "host:port"
// localAddr (empty string = OS chooses the source address), or an error to
// reject the session.
type DeciderFunc func(local net.Addr, peer net.Addr) (dest string, localAddr string, err error)

// Logger is the minimal structured logging interface expected by the proxies.
// Pass nil to disable all logging.
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// noopLogger silently discards every log message.
type noopLogger struct{}

func (noopLogger) Debugf(_ string, _ ...interface{}) {}
func (noopLogger) Infof(_ string, _ ...interface{})  {}
func (noopLogger) Warnf(_ string, _ ...interface{})  {}
func (noopLogger) Errorf(_ string, _ ...interface{}) {}

// resolveLogger returns l unchanged if non-nil, otherwise a noopLogger.
func resolveLogger(l Logger) Logger {
	if l == nil {
		return noopLogger{}
	}
	return l
}

// ─── Configuration ────────────────────────────────────────────────────────────

// Config holds tunable parameters shared by both proxy types.
type Config struct {
	// MaxSessions is the maximum number of concurrent sessions (0 = unlimited).
	MaxSessions int

	// ReadTimeout closes a session after this duration with no bytes received
	// from src.  The deadline is rolled forward on every successful read, so
	// only truly silent sessions are evicted.
	// Constructors default to 5 min for both TCP and UDP.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time allowed for a single write to complete
	// before the session is torn down.  It guards against a stalled destination
	// holding a goroutine open indefinitely.
	// Constructors default to 30s for TCP and UDP.
	WriteTimeout time.Duration

	// BufferSize is the size of copy buffers used by TCP pipes (bytes).
	// Not used by UDP (UDP always uses 64 KiB buffers to handle max-sized datagrams).
	// Each TCP session uses two buffers for bidirectional copying.
	// Defaults to 32 KiB when <= 0.
	BufferSize int

	// DialTimeout is the maximum time the TCP proxy waits when dialling the
	// upstream destination for a new session.  The dial is also cancelled
	// immediately whenever Shutdown is called, regardless of this value.
	// Defaults to 10 s when <= 0.
	DialTimeout time.Duration
}

const DEFAULT_DIAL_TIMEOUT = 10 * time.Second
const DEFAULT_BUFFER_SIZE = 32 * 1024
const DEFAULT_MAX_SESSIONS = 2048
const DEFAULT_READ_TIMEOUT = 5 * time.Minute
const DEFAULT_WRITE_TIMEOUT = 30 * time.Second

// DefaultConfig returns a sensible default Config.
func DefaultConfig() Config {
	return Config{
		MaxSessions:  DEFAULT_MAX_SESSIONS,
		BufferSize:   DEFAULT_BUFFER_SIZE,
		DialTimeout:  DEFAULT_DIAL_TIMEOUT,
		ReadTimeout:  DEFAULT_READ_TIMEOUT,
		WriteTimeout: DEFAULT_WRITE_TIMEOUT,
	}
}

// ─── ConnContext ──────────────────────────────────────────────────────────────

// ConnContext holds all observable state for one proxy session.
//
// The counters are updated atomically and are safe for concurrent reads.
type ConnContext struct {
	// ID is a monotonically increasing session identifier (starts at 1).
	ID uint64
	// PeerAddr is the connecting client's address.
	PeerAddr net.Addr
	// DestAddr is the upstream host:port chosen by DeciderFunc.
	DestAddr string
	// CreatedAt is the wall-clock time the session was established.
	CreatedAt time.Time

	// lastSeen stores a UnixNano timestamp updated on every transferred packet/byte.
	lastSeen atomic.Int64

	// BytesIn counts bytes forwarded from upstream to the client.
	BytesIn atomic.Uint64
	// BytesOut counts bytes forwarded from the client to upstream.
	BytesOut atomic.Uint64
	// PacketsIn counts UDP datagrams forwarded from upstream to the client.
	PacketsIn atomic.Uint64
	// PacketsOut counts UDP datagrams forwarded from the client to upstream.
	PacketsOut atomic.Uint64

	// cancel closes the session's context.
	cancel func()
}

// newConnContext allocates a ConnContext and initialises lastSeen to now.
func newConnContext(id uint64, peer net.Addr, dest string, cancel func()) *ConnContext {
	now := time.Now()
	c := &ConnContext{
		ID:        id,
		PeerAddr:  peer,
		DestAddr:  dest,
		CreatedAt: now,
		cancel:    cancel,
	}
	c.lastSeen.Store(now.UnixNano())
	return c
}

// LastSeen returns the time of the most recently observed packet or byte.
func (c *ConnContext) LastSeen() time.Time {
	return time.Unix(0, c.lastSeen.Load())
}

// touch updates lastSeen to the current time.
func (c *ConnContext) touch() {
	c.lastSeen.Store(time.Now().UnixNano())
}

// Close cancels the session.  Safe to call multiple times.
func (c *ConnContext) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}

// ─── Metrics ──────────────────────────────────────────────────────────────────

// Metrics is a snapshot of session cache statistics.
type Metrics struct {
	ActiveSessions uint64
	TotalCreated   uint64
	TotalClosed    uint64
}

func (m Metrics) String() string {
	return fmt.Sprintf("active=%d created=%d closed=%d",
		m.ActiveSessions, m.TotalCreated, m.TotalClosed)
}

// ─── Session cache ────────────────────────────────────────────────────────────

// sessionCache is a concurrent-safe registry of live ConnContexts together
// with aggregate lifetime metrics.
type sessionCache struct {
	mu      sync.RWMutex
	entries map[uint64]*ConnContext

	totalCreated atomic.Uint64
	totalClosed  atomic.Uint64
}

func newSessionCache() *sessionCache {
	return &sessionCache{entries: make(map[uint64]*ConnContext, 64)}
}

// add registers a new session.
func (c *sessionCache) add(ctx *ConnContext) {
	c.mu.Lock()
	c.entries[ctx.ID] = ctx
	c.mu.Unlock()
	c.totalCreated.Add(1)
}

// remove unregisters a session by ID.  It is idempotent.
func (c *sessionCache) remove(id uint64) {
	c.mu.Lock()
	if _, ok := c.entries[id]; ok {
		delete(c.entries, id)
		c.totalClosed.Add(1)
	}
	c.mu.Unlock()
}

// get retrieves a session by ID.
func (c *sessionCache) get(id uint64) (*ConnContext, bool) {
	c.mu.RLock()
	ctx, ok := c.entries[id]
	c.mu.RUnlock()
	return ctx, ok
}

// len returns the current number of active sessions.
func (c *sessionCache) len() int {
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	return n
}

// metrics returns a consistent metrics snapshot.
func (c *sessionCache) metrics() Metrics {
	c.mu.RLock()
	active := uint64(len(c.entries))
	c.mu.RUnlock()
	return Metrics{
		ActiveSessions: active,
		TotalCreated:   c.totalCreated.Load(),
		TotalClosed:    c.totalClosed.Load(),
	}
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// idCounter is a global monotonic session ID source.
var idCounter atomic.Uint64

// nextID returns the next unique session ID (1-based).
func nextID() uint64 {
	return idCounter.Add(1)
}
