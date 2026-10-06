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
| Interface binding via `SO_BINDTODEVICE` (Linux) | ✓ | ✓ |
| Relaying via upstream proxy (HTTP CONNECT, SOCKS4/4a, SOCKS5) | ✓ | — |
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
// LocalBinding carries the local-side binding parameters for an outbound proxy
// connection.  All fields are optional and may be set independently.
type LocalBinding struct {
    // IP is the local source address to bind the outgoing socket to.
    // If nil, the OS selects an appropriate source address.
    IP net.IP

    // Interface is the name of the network interface (e.g. "eth0") to bind
    // the outgoing socket to via SO_BINDTODEVICE (Linux only).
    // An empty string disables interface-level binding.
    Interface string

    // AltIP is an optional local source address of the other IP version
    // than IP.  It is used instead of IP when the remote address is of that
    // IP version, e.g. when the upstream proxy server is reached via another
    // IP version than the destination.
    AltIP net.IP

    // Upstream, if set, relays the outbound TCP connection through a proxy
    // server instead of connecting to the destination directly.  IP and
    // Interface then apply to the connection to the proxy server, except
    // when the proxy server is reached via loopback.
    // UDP sessions with an upstream proxy are rejected.
    Upstream *UpstreamProxy
}

// UpstreamProxy describes a proxy server that outbound TCP sessions are
// relayed through.
type UpstreamProxy struct {
    // URL is the proxy server address, as returned by ParseUpstreamProxyURL.
    URL *url.URL

    // Host is the optional hostname of the destination.  It is sent to the
    // proxy instead of the destination IP if the protocol supports it.
    Host string
}

// ParseUpstreamProxyURL parses and validates an upstream proxy URL.
// Returned errors never contain the URL itself, as it may carry credentials.
func ParseUpstreamProxyURL(s string) (*url.URL, error)

// SupportedUpstreamProxySchemes returns the supported URL schemes.
func SupportedUpstreamProxySchemes() []string

// DeciderFunc is called once per new session to determine the upstream
// destination and optional local binding parameters for the outgoing socket.
//
// local is the proxy's listen address; peer is the connecting client's address.
//
// It returns:
//   - remoteIP:   required upstream IP address.
//   - remotePort: required upstream port.
//   - binding:    optional local binding; nil lets the OS choose freely.
//                 Set binding.IP to pin the source address, binding.Interface
//                 to restrict the socket to a specific network device (Linux).
//   - extraInfo:  optional caller-defined value attached to the session's ConnContext.
//   - err:        non-nil rejects the session without dialling upstream.
type DeciderFunc func(local net.Addr, peer net.Addr) (
    remoteIP   net.IP,
    remotePort uint16,
    binding    *LocalBinding,
    extraInfo  any,
    err        error,
)

// Logger is the minimal interface accepted by both proxies.
// Pass nil to suppress all log output.
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}

// ConnContext holds observable state for one proxy session.
// Counters are updated atomically and safe for concurrent reads.
type ConnContext struct {
    BytesIn    atomic.Uint64 // bytes forwarded upstream → client
    BytesOut   atomic.Uint64 // bytes forwarded client → upstream
    PacketsIn  atomic.Uint64 // UDP datagrams upstream → client
    PacketsOut atomic.Uint64 // UDP datagrams client → upstream
}

func (c *ConnContext) ID()        uint64
func (c *ConnContext) PeerAddr()  net.Addr
func (c *ConnContext) DestIP()    net.IP
func (c *ConnContext) DestPort()  uint16
func (c *ConnContext) CreatedAt() time.Time
func (c *ConnContext) LastSeen()  time.Time
func (c *ConnContext) ExtraInfo() any
func (c *ConnContext) Close()            // cancels the session
```

### Constructors

```go
// TCP — uses DefaultConfig
func NewTCPProxy(listenAddr string, network string, decider DeciderFunc, logger Logger, logPrefix string) (*TCPProxy, error)

// TCP — custom configuration
func NewTCPProxyWithConfig(listenAddr string, network string, decider DeciderFunc, logger Logger, cfg Config, logPrefix string) (*TCPProxy, error)

// UDP — uses DefaultConfig
func NewUDPProxy(listenAddr string, network string, decider DeciderFunc, logger Logger, logPrefix string) (*UDPProxy, error)

// UDP — custom configuration
func NewUDPProxyWithConfig(listenAddr string, network string, decider DeciderFunc, logger Logger, cfg Config, logPrefix string) (*UDPProxy, error)
```

Both constructors bind the socket and start background goroutines immediately.
They return an error if binding fails or if `decider` is nil.

### Address

```go
func (p *TCPProxy) Addr() net.Addr
func (p *UDPProxy) Addr() net.Addr
```

Returns the address the proxy is currently listening on.

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

### Session lookup

```go
// Returns all active sessions whose egress destination matches destIP:destPort.
// The egress destination is the address the outgoing socket connects to: the
// upstream destination, or the upstream proxy server if one is used.
// Returns nil if none exist.
func (p *TCPProxy) FindProxiedEgressConnection(destIP net.IP, destPort uint16) []*ConnContext
func (p *UDPProxy) FindProxiedEgressConnection(destIP net.IP, destPort uint16) []*ConnContext
```

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
decider := func(local, peer net.Addr) (net.IP, uint16, *proxy.LocalBinding, any, error) {
    return net.ParseIP("192.168.1.10"), 8080, nil, nil, nil
}

p, err := proxy.NewTCPProxy(":8080", "tcp4", decider, nil, "tcp proxy IPv4")
if err != nil {
    log.Fatal(err)
}

// Graceful shutdown on SIGTERM with a 10-second drain window.
sig := make(chan os.Signal, 1)
signal.Notify(sig, syscall.SIGTERM)
<-sig

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
p.Shutdown(ctx)
```

### Per-client routing with source-address and interface binding (split tunnelling)

```go
decider := func(local, peer net.Addr) (net.IP, uint16, *proxy.LocalBinding, any, error) {
    host, _, _ := net.SplitHostPort(peer.String())
    if isTunnelledIP(host) {
        // Route through the physical interface, binding the source address and
        // restricting the socket to that device so traffic bypasses the VPN.
        return directGatewayIP, 443, &proxy.LocalBinding{
            IP:        net.ParseIP("192.168.1.5"), // physical interface address
            Interface: "eth0",                     // Linux: SO_BINDTODEVICE
        }, nil, nil
    }
    return vpnGatewayIP, 443, nil, nil, nil
}

p, err := proxy.NewTCPProxy(":443", "tcp4", decider, myLogger, "tcp proxy IPv4")
```

### Relaying through an upstream proxy

```go
proxyURL, err := proxy.ParseUpstreamProxyURL("socks5h://user:password@127.0.0.1:1080")
if err != nil {
    return err
}

decider := func(local, peer net.Addr) (net.IP, uint16, *proxy.LocalBinding, any, error) {
    // The proxy receives "example.com:443" instead of the IP, as the
    // "socks5h" scheme lets the proxy resolve the hostname.
    return destIP, 443, &proxy.LocalBinding{
        Upstream: &proxy.UpstreamProxy{URL: proxyURL, Host: "example.com"},
    }, nil, nil
}
```

Supported schemes:

| Scheme | Protocol | Destination sent to the proxy | Authentication |
|--------|----------|-------------------------------|----------------|
| `http` | HTTP CONNECT | hostname if known, else IP | Basic (`user:password@`) |
| `socks4` | SOCKS4 | IPv4 only | user ID (`user@`) |
| `socks4a` | SOCKS4a | hostname if known, else IPv4 | user ID (`user@`) |
| `socks5` | SOCKS5 | IP | username/password (`user:password@`) |
| `socks5h` | SOCKS5 | hostname if known, else IP | username/password (`user:password@`) |

### UDP proxy with custom timeouts

```go
cfg := proxy.DefaultConfig()
cfg.ReadTimeout = 30 * time.Second
cfg.MaxSessions = 1024

p, err := proxy.NewUDPProxyWithConfig(":5353", "udp4", decider, myLogger, cfg, "udp proxy IPv4")
```

---

## Running tests and benchmarks

```sh
# Unit tests
go test ./...

# Race detector
go test -race ./...

# Benchmarks with allocation reporting
go test -run='^$' -bench=Benchmark -benchmem    # All benchmarks
go test -run='^$' -bench=BenchmarkTCP -benchmem # TCP only
go test -run='^$' -bench=BenchmarkUDP -benchmem # UDP only
```

---

## Design notes

* **Pooled buffers** — TCP pipes use a `sync.Pool` of 32 KiB `[]byte` slices;
  the UDP path uses a separate pool of 64 KiB slices (maximum UDP payload).
  Both avoid per-transfer heap allocations in steady state.
* **Goroutine budget** — the TCP proxy spawns four goroutines per session: one
  session handler, two bidirectional copy goroutines (one per direction), and
  one watchdog; the UDP proxy spawns one goroutine per session (upstream
  reader) plus two shared goroutines (inbound read loop and idle cleanup loop).
  All goroutines are tracked via a `sync.WaitGroup`.
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
* **Interface binding (Linux)** — when `LocalBinding.Interface` is non-empty,
  `SO_BINDTODEVICE` is set on the outgoing socket via `net.Dialer.Control`
  before `connect(2)`.  This forces the kernel to route the connection through
  the named device regardless of the routing table, which is required for
  split-tunnelling when a default VPN route would otherwise capture the traffic.
  On non-Linux platforms the field is ignored (no-op).
* **Upstream proxies** — the connection to the proxy server is a plain TCP
  connection, the handshake is performed on it directly and it is then used for
  the tunnel, so half-close keeps working.  HTTP responses are read byte by
  byte to never consume tunnelled data that follows the response header.  The
  proxy server's address is registered as the session's egress destination
  before dialling, so the firewall can recognise the outgoing connection.
  The dial timeout covers both the TCP connect and the handshake.  If the
  proxy hostname resolves to multiple addresses, they are tried in order
  (IPv4 first, unless the binding prefers IPv6), so e.g. "localhost" works
  with proxies that only listen on 127.0.0.1.

