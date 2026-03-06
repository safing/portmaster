# proxy

Internal Layer-4 TCP and UDP proxy package used by the split-tunnelling
subsystem.  Provides injected routing decisions, session tracking, and graceful
shutdown.

---

## Overview

| Feature | TCP | UDP |
|---------|-----|-----|
| Routing via `DeciderFunc` | ✓ | ✓ |
| Optional source-address binding | ✓ | ✓ |
| Session tracking + metrics | ✓ | ✓ |
| Pooled copy buffers | ✓ | ✓ |
| Graceful shutdown | ✓ | ✓ |
| Max sessions limit | ✓ | ✓ |
| Read/write deadlines | ✓ | ✓ |
| Idle eviction (cleanup loop) | — | ✓ |
| Bidirectional, half-close | ✓ | n/a |

---

## API

### Types

```go
// DeciderFunc is called once per new session to determine the upstream
// address and the optional local address to bind the outgoing connection to.
// local is the proxy's listen address; peer is the connecting client's address.
// Return a non-empty localAddr to pin the outgoing connection to a specific
// source address; return "" to let the OS choose.
// Return an error to reject the session.
type DeciderFunc func(local net.Addr, peer net.Addr) (dest string, localAddr string, err error)

// Logger is the minimal interface accepted by both proxies.
// Pass nil to suppress all log output.
type Logger interface {
    Debugf(format string, args ...interface{})
    Infof(format string, args ...interface{})
    Warnf(format string, args ...interface{})
    Errorf(format string, args ...interface{})
}
```

### Constructors

```go
// TCP — uses DefaultConfig
func NewTCPProxy(listenAddr string, decider DeciderFunc, logger Logger) (*TCPProxy, error)

// TCP — custom configuration
func NewTCPProxyWithConfig(listenAddr string, decider DeciderFunc, logger Logger, cfg Config) (*TCPProxy, error)

// UDP — uses DefaultConfig
func NewUDPProxy(listenAddr string, decider DeciderFunc, logger Logger) (*UDPProxy, error)

// UDP — custom configuration
func NewUDPProxyWithConfig(listenAddr string, decider DeciderFunc, logger Logger, cfg Config) (*UDPProxy, error)
```

Both constructors bind the socket and start background goroutines immediately.
They return an error if binding fails or if `decider` is nil.

### Configuration

```go
type Config struct {
    // MaxSessions is the maximum number of concurrent sessions (0 = unlimited).
    // Default: 2048.
    MaxSessions int

    // ReadTimeout closes a session after this duration with no bytes received
    // from the source.  The deadline is rolled forward on every successful
    // read, so only truly silent sessions are evicted.
    // Default: 5 min.
    ReadTimeout time.Duration

    // WriteTimeout is the maximum time allowed for a single write to complete.
    // Guards against a stalled destination holding a goroutine open.
    // Default: 30 s.
    WriteTimeout time.Duration

    // BufferSize is the size of copy buffers used by the TCP pipe (bytes).
    // UDP always uses 64 KiB buffers regardless of this setting.
    // Default: 32 KiB.
    BufferSize int

    // DialTimeout is the maximum time the TCP proxy waits when dialling the
    // upstream destination.  Default: 10 s.
    DialTimeout time.Duration
}

func DefaultConfig() Config
```

### Shutdown

```go
func (p *TCPProxy) Shutdown(ctx context.Context) error
func (p *UDPProxy) Shutdown(ctx context.Context) error
```

Closes the listen socket, cancels all active sessions, and waits for
goroutines to drain.  If `ctx` expires first, the method returns
`ctx.Err()` but goroutines are still cleaning up (they are not leaked).

### Metrics

```go
type Metrics struct {
    ActiveSessions uint64
    TotalCreated   uint64
    TotalClosed    uint64
}

func (p *TCPProxy) Metrics() Metrics
func (p *UDPProxy) Metrics() Metrics
```

---

## Usage examples

### Transparent TCP proxy (always route to a fixed backend)

```go
decider := func(local, peer net.Addr) (string, string, error) {
    return "backend.internal:8080", "", nil
}

proxy, err := proxy.NewTCPProxy(":8080", decider, nil)
if err != nil {
    log.Fatal(err)
}

// Graceful shutdown on SIGTERM with a 10-second drain window.
sig := make(chan os.Signal, 1)
signal.Notify(sig, syscall.SIGTERM)
<-sig

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
proxy.Shutdown(ctx)
```

### Per-client routing with source-address binding (split tunnelling)

```go
decider := func(local, peer net.Addr) (string, string, error) {
    host, _, _ := net.SplitHostPort(peer.String())
    if isTunnelledIP(host) {
        // Route through VPN interface, binding source to its address.
        return "vpn-gateway:443", "10.0.0.1:0", nil
    }
    return "direct-gateway:443", "", nil
}

proxy, err := proxy.NewTCPProxy(":443", decider, myLogger)
```

### UDP proxy with custom timeouts

```go
cfg := proxy.DefaultConfig()
cfg.ReadTimeout  = 30 * time.Second
cfg.MaxSessions  = 1024

p, err := proxy.NewUDPProxyWithConfig(":5353", decider, myLogger, cfg)
```

---

## Running tests and benchmarks

```sh
# Unit tests
go test ./...

# Race detector
go test -race ./...

# Benchmarks with allocation reporting
go test -run='^$' -bench=. -benchmem ./...
```

---

## Design notes

* **Pooled buffers** — TCP pipes use a `sync.Pool` of 32 KiB `[]byte` slices;
  the UDP path uses a separate pool of 64 KiB slices (maximum UDP payload).
  Both avoid per-transfer heap allocations in steady state.
* **Goroutine budget** — the TCP proxy spawns two goroutines per session (one
  per direction) plus a shutdown watchdog; the UDP proxy spawns one goroutine
  per session (upstream reader) plus a shared cleanup loop.  All goroutines are
  tracked via a `sync.WaitGroup`.
* **Half-close** — when one TCP peer closes its write side, the proxy attempts
  `CloseWrite` on the upstream, enabling proper FIN propagation.
* **NAT session table** — UDP sessions are keyed by the client's `"ip:port"`
  string.  A double-checked locking pattern prevents duplicate sessions under
  burst traffic.
* **UDP write deadline sharing** — all upstream-to-client goroutines write on
  the same shared listen socket.  Each goroutine sets a rolling write deadline
  immediately before its own write, so concurrent sessions can shift each
  other's deadline by at most `WriteTimeout`.  This is an accepted trade-off of
  the single-socket UDP design.
* **Context propagation** — the proxy's top-level `context.Context` is the
  parent of every session context, so a single `Shutdown` call cascades to
  all live sessions.
