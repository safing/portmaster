package proxy

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ─── ConnContext ──────────────────────────────────────────────────────────────

// ConnContext holds all observable state for one proxy session.
//
// The counters are updated atomically and are safe for concurrent reads.
type ConnContext struct {
	// id is a monotonically increasing session identifier (starts at 1).
	id uint64
	// peerAddr is the connecting client's address.
	peerAddr net.Addr
	// destIP is the upstream IP address chosen by DeciderFunc.
	destIP net.IP
	// destPort is the upstream port chosen by DeciderFunc.
	destPort uint16
	// createdAt is the wall-clock time the session was established.
	createdAt time.Time

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

	// extraInfo is an optional user-defined object returned by DeciderFunc.
	// It is set once at session creation and never modified.
	extraInfo any

	// cancel closes the session's context.
	cancel func()
}

// newConnContext allocates a ConnContext and initialises lastSeen to now.
// destIP is normalised to 16-byte form (IPv4-in-IPv6) for consistent indexing.
func newConnContext(id uint64, peer net.Addr, destIP net.IP, destPort uint16, cancel func(), extraInfo any) *ConnContext {
	now := time.Now()
	c := &ConnContext{
		id:        id,
		peerAddr:  peer,
		destIP:    destIP.To16(),
		destPort:  destPort,
		createdAt: now,
		cancel:    cancel,
		extraInfo: extraInfo,
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

// ID returns the session's monotonically increasing identifier (starts at 1).
func (c *ConnContext) ID() uint64 { return c.id }

// PeerAddr returns the connecting client's address.
func (c *ConnContext) PeerAddr() net.Addr { return c.peerAddr }

// DestIP returns the upstream IP address chosen by DeciderFunc.
func (c *ConnContext) DestIP() net.IP { return c.destIP }

// DestPort returns the upstream port chosen by DeciderFunc.
func (c *ConnContext) DestPort() uint16 { return c.destPort }

// CreatedAt returns the wall-clock time the session was established.
func (c *ConnContext) CreatedAt() time.Time { return c.createdAt }

// ExtraInfo returns the optional user-defined object returned by DeciderFunc.
// It is set once at session creation and never modified.
func (c *ConnContext) ExtraInfo() any { return c.extraInfo }

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

// destKey is the secondary-index key used to look up sessions by upstream
// destination.  Using a fixed-size struct as a map key avoids string allocation
// and gives O(1) hashing.
type destKey struct {
	ip   [16]byte // IPv4-in-IPv6 form (To16)
	port uint16
}

// makeDestKey builds a destKey from a pre-parsed IP and port.
// Returns (key, false) if ip is nil.
func makeDestKey(ip net.IP, port uint16) (destKey, bool) {
	ip16 := ip.To16()
	if ip16 == nil {
		return destKey{}, false
	}
	var k destKey
	copy(k.ip[:], ip16)
	k.port = port
	return k, true
}

// sessionCache is a concurrent-safe registry of live ConnContexts together
// with aggregate lifetime metrics.
type sessionCache struct {
	mu      sync.RWMutex
	entries map[uint64]*ConnContext
	// byDest is a secondary index: destKey → set of ConnContexts.
	// It allows FindProxiedEgressConnection to skip iterating all entries.
	byDest map[destKey]map[uint64]*ConnContext

	totalCreated atomic.Uint64
	totalClosed  atomic.Uint64
}

func newSessionCache() *sessionCache {
	return &sessionCache{
		entries: make(map[uint64]*ConnContext, 64),
		byDest:  make(map[destKey]map[uint64]*ConnContext),
	}
}

// add registers a new session.
func (c *sessionCache) add(ctx *ConnContext) {
	k, hasKey := makeDestKey(ctx.destIP, ctx.destPort)
	c.mu.Lock()
	c.entries[ctx.id] = ctx
	if hasKey {
		inner := c.byDest[k]
		if inner == nil {
			inner = make(map[uint64]*ConnContext, 1)
			c.byDest[k] = inner
		}
		inner[ctx.id] = ctx
	}
	c.mu.Unlock()
	c.totalCreated.Add(1)
}

// remove unregisters a session.  It is idempotent.
func (c *sessionCache) remove(ctx *ConnContext) {
	c.mu.Lock()
	if _, ok := c.entries[ctx.id]; ok {
		delete(c.entries, ctx.id)
		if k, hasKey := makeDestKey(ctx.destIP, ctx.destPort); hasKey {
			inner := c.byDest[k]
			delete(inner, ctx.id)
			if len(inner) == 0 {
				delete(c.byDest, k)
			}
		}
		c.totalClosed.Add(1)
	}
	c.mu.Unlock()
}

// findByDest returns all active sessions whose upstream destination matches
// destIP and destPort.  Returns nil if no matching session exists.
func (c *sessionCache) findByDest(destIP net.IP, destPort uint16) []*ConnContext {
	ip16 := destIP.To16()
	if ip16 == nil {
		return nil
	}
	var k destKey
	copy(k.ip[:], ip16)
	k.port = destPort

	c.mu.RLock()
	inner := c.byDest[k]
	if len(inner) == 0 {
		c.mu.RUnlock()
		return nil
	}
	result := make([]*ConnContext, 0, len(inner))
	for _, ctx := range inner {
		result = append(result, ctx)
	}
	c.mu.RUnlock()
	return result
}

// hasByDest checks if there is an active session whose upstream destination
// matches destIP and destPort.  Returns false if no matching session exists.
func (c *sessionCache) hasByDest(destIP net.IP, destPort uint16) bool {
	ip16 := destIP.To16()
	if ip16 == nil {
		return false
	}
	var k destKey
	copy(k.ip[:], ip16)
	k.port = destPort

	c.mu.RLock()
	has := len(c.byDest[k]) > 0
	c.mu.RUnlock()
	return has
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
