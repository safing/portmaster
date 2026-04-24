// Package proxy provides minimal, Layer-4 TCP and UDP proxies
// with injected routing decisions (DeciderFunc), structured logging, session
// tracking, and graceful shutdown.
package proxy

import (
	"net"
	"time"
)

// ─── Public API types ────────────────────────────────────────────────────────

// DeciderFunc is called once per new session to determine the upstream
// destination address, the optional local address to bind the outgoing
// connection to, and an optional extra context object.
//
// local is the proxy's listen address; peer is the connecting client's
// address.
//
// It returns:
//   - remoteIP: required upstream IP address
//   - remotePort: required upstream port
//   - localIP: optional local IP to use as the source address (nil = OS chooses)
//   - extraInfo: optional user-defined object attached to the session context
//   - err: non-nil rejects the session
type DeciderFunc func(local net.Addr, peer net.Addr) (remoteIP net.IP, remotePort uint16, localIP net.IP, extraInfo any, err error)

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
