package firewall

import (
	"math"
	"time"

	"github.com/safing/portmaster/service/network/packet"
)

const (
	// pendingBandwidthTTL is how long additive bandwidth updates of an unknown
	// connection are held for it. The integrations report every second, so
	// this covers several reports while keeping bytes of predecessors with the
	// same 5-tuple from being credited to a successor connection.
	pendingBandwidthTTL = 10 * time.Second

	// pendingBandwidthMaxEntries is the maximum number of connection IDs
	// that bandwidth is held for. Updates of further connection IDs are
	// dropped, as they were before pending bandwidth was kept.
	pendingBandwidthMaxEntries = 4096

	// pendingBandwidthSweepInterval is the minimum interval between removals
	// of expired entries.
	pendingBandwidthSweepInterval = time.Second
)

// pendingBandwidthEntry holds the bytes of additive bandwidth updates that
// could not be attributed to a connection.
type pendingBandwidthEntry struct {
	bytesReceived uint64
	bytesSent     uint64
	// firstHeld is when the first held update arrived. The whole entry
	// expires after pendingBandwidthTTL from then.
	firstHeld time.Time
}

// pendingBandwidth holds additive bandwidth updates for connections that are
// not known (yet) and adds them to the next update of the connection.
//
// Additive updates only hold the bytes counted since the previous update.
// When no connection is found for an update, eg. because the integration
// counted bytes before the connection was registered, these bytes would be
// lost for good. They are kept for a short time only, so that bytes of long
// gone connections with the same connection ID are dropped instead of being
// credited to a successor connection.
//
// Absolute updates are not held, as the next update includes their bytes.
//
// It is not safe for concurrent use. It is owned by the bandwidth update
// handler.
type pendingBandwidth struct {
	entries   map[string]*pendingBandwidthEntry
	lastSweep time.Time
}

func newPendingBandwidth() *pendingBandwidth {
	return &pendingBandwidth{
		entries: make(map[string]*pendingBandwidthEntry),
	}
}

// resolve handles the held bytes for the given bandwidth update.
// If connFound is false, the bytes of an additive update are held and nil is
// returned. If connFound is true, the held bytes of the connection are removed
// and returned together with the update, if they did not expire.
func (p *pendingBandwidth) resolve(bwUpdate *packet.BandwidthUpdate, connFound bool, now time.Time) *packet.BandwidthUpdate {
	if now.Sub(p.lastSweep) >= pendingBandwidthSweepInterval || now.Before(p.lastSweep) {
		p.sweep(now)
	}

	if bwUpdate.Method != packet.Additive {
		if !connFound {
			return nil
		}
		return bwUpdate
	}

	if !connFound {
		p.hold(bwUpdate, now)
		return nil
	}

	entry, ok := p.entries[bwUpdate.ConnID]
	if !ok {
		return bwUpdate
	}
	delete(p.entries, bwUpdate.ConnID)
	if p.expired(entry, now) {
		return bwUpdate
	}

	return &packet.BandwidthUpdate{
		ConnID:        bwUpdate.ConnID,
		BytesReceived: saturatingAdd(bwUpdate.BytesReceived, entry.bytesReceived),
		BytesSent:     saturatingAdd(bwUpdate.BytesSent, entry.bytesSent),
		Method:        bwUpdate.Method,
	}
}

// hold adds the bytes of the additive update to the held bytes of its
// connection ID.
func (p *pendingBandwidth) hold(bwUpdate *packet.BandwidthUpdate, now time.Time) {
	entry, ok := p.entries[bwUpdate.ConnID]
	if ok && p.expired(entry, now) {
		// Do not extend the lifetime of expired bytes.
		delete(p.entries, bwUpdate.ConnID)
		ok = false
	}
	if !ok {
		if len(p.entries) >= pendingBandwidthMaxEntries {
			p.sweep(now)
			if len(p.entries) >= pendingBandwidthMaxEntries {
				// Drop the update rather than evicting entries that are still
				// valid.
				return
			}
		}
		entry = &pendingBandwidthEntry{firstHeld: now}
		p.entries[bwUpdate.ConnID] = entry
	}

	entry.bytesReceived = saturatingAdd(entry.bytesReceived, bwUpdate.BytesReceived)
	entry.bytesSent = saturatingAdd(entry.bytesSent, bwUpdate.BytesSent)
}

// sweep removes expired entries.
func (p *pendingBandwidth) sweep(now time.Time) {
	p.lastSweep = now
	for connID, entry := range p.entries {
		if p.expired(entry, now) {
			delete(p.entries, connID)
		}
	}
}

// expired returns whether the held bytes of the entry are too old to be
// credited to a connection.
func (p *pendingBandwidth) expired(entry *pendingBandwidthEntry, now time.Time) bool {
	return now.Sub(entry.firstHeld) > pendingBandwidthTTL
}

// saturatingAdd returns a+b, or the maximum value if that overflows.
func saturatingAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}
