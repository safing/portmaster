package firewall

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// deliverBandwidth handles the update like updateBandwidth does, with conn
// being the connection found for it, or nil if none is found.
// It returns the bytes applied to the connection.
func deliverBandwidth(t *testing.T, p *pendingBandwidth, conn *network.Connection, bwUpdate *packet.BandwidthUpdate, now time.Time) (bytesIn, bytesOut uint64, applied bool) {
	t.Helper()

	bwUpdate = p.resolve(bwUpdate, conn != nil, now)
	if conn == nil {
		if bwUpdate != nil {
			t.Fatal("update of an unknown connection must not be returned")
		}
		return 0, 0, false
	}
	bytesIn, bytesOut, applied = applyBandwidthUpdate(conn, bwUpdate)
	if applied {
		conn.Unlock()
	}
	return bytesIn, bytesOut, applied
}

func additiveUpdate(connID string, received, sent uint64) *packet.BandwidthUpdate {
	return &packet.BandwidthUpdate{
		ConnID:        connID,
		BytesReceived: received,
		BytesSent:     sent,
		Method:        packet.Additive,
	}
}

func expectApplied(t *testing.T, gotIn, gotOut uint64, applied bool, wantIn, wantOut uint64) {
	t.Helper()

	if !applied {
		t.Fatal("update was not applied")
	}
	if gotIn != wantIn || gotOut != wantOut {
		t.Errorf("applied %d/%d bytes, want %d/%d", gotIn, gotOut, wantIn, wantOut)
	}
}

// testConnID is a connection ID as the bandwidth integrations create it.
var testConnID = packet.CreateConnectionID(
	packet.UDP,
	[]byte{127, 0, 0, 1}, 1000,
	[]byte{8, 8, 8, 8}, 53,
	false,
)

// TestPendingBandwidthCreditedOnce checks that the bytes of additive updates
// that arrive before their connection is known are credited to the connection
// once it is found, and only once.
func TestPendingBandwidthCreditedOnce(t *testing.T) {
	t.Parallel()

	p := newPendingBandwidth()
	conn := &network.Connection{}
	start := time.Unix(1_000_000, 0)

	// The connection is not known yet.
	deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 100, 10), start)

	// The connection is found with the next update.
	bytesIn, bytesOut, applied := deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 5, 1), start.Add(time.Second))
	expectApplied(t, bytesIn, bytesOut, applied, 105, 11)

	// The held bytes are not credited again.
	bytesIn, bytesOut, applied = deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 5, 1), start.Add(2*time.Second))
	expectApplied(t, bytesIn, bytesOut, applied, 5, 1)
	if conn.BytesReceived != 110 || conn.BytesSent != 12 {
		t.Errorf("connection has %d/%d bytes, want 110/12", conn.BytesReceived, conn.BytesSent)
	}
	if len(p.entries) != 0 {
		t.Errorf("%d entries are still held", len(p.entries))
	}
}

// TestPendingBandwidthAccumulates checks that several updates of an unknown
// connection are added up.
func TestPendingBandwidthAccumulates(t *testing.T) {
	t.Parallel()

	p := newPendingBandwidth()
	conn := &network.Connection{}
	start := time.Unix(1_000_000, 0)

	for i := range 5 {
		deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 100, 10), start.Add(time.Duration(i)*time.Second))
	}
	bytesIn, bytesOut, applied := deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 1, 1), start.Add(5*time.Second))
	expectApplied(t, bytesIn, bytesOut, applied, 501, 51)

	// Held bytes do not wrap around.
	deliverBandwidth(t, p, nil, additiveUpdate(testConnID, math.MaxUint64-1, 0), start)
	deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 2, 0), start)
	bwUpdate := p.resolve(additiveUpdate(testConnID, 1, 0), true, start)
	if bwUpdate.BytesReceived != math.MaxUint64 {
		t.Errorf("got %d received bytes, want %d", bwUpdate.BytesReceived, uint64(math.MaxUint64))
	}
}

// TestPendingBandwidthExpires checks that held bytes older than the TTL are
// dropped instead of being credited to a connection, as they may belong to a
// predecessor with the same connection ID.
func TestPendingBandwidthExpires(t *testing.T) {
	t.Parallel()

	start := time.Unix(1_000_000, 0)
	afterTTL := start.Add(pendingBandwidthTTL + time.Second)

	// Expired when the connection is found.
	p := newPendingBandwidth()
	conn := &network.Connection{}
	deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 100, 10), start)
	bytesIn, bytesOut, applied := deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 5, 1), afterTTL)
	expectApplied(t, bytesIn, bytesOut, applied, 5, 1)
	if len(p.entries) != 0 {
		t.Errorf("%d entries are still held", len(p.entries))
	}

	// Removed by the sweep, triggered by any update.
	p = newPendingBandwidth()
	deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 100, 10), start)
	deliverBandwidth(t, p, nil, additiveUpdate("other", 1, 1), afterTTL)
	if _, ok := p.entries[testConnID]; ok {
		t.Error("expired entry was not removed")
	}

	// Further updates do not extend the lifetime of held bytes: Only what was
	// held within the TTL is credited.
	p = newPendingBandwidth()
	conn = &network.Connection{}
	for i := range 2 * int(pendingBandwidthTTL/time.Second) {
		deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 100, 0), start.Add(time.Duration(i)*time.Second))
	}
	bytesIn, bytesOut, applied = deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 1, 0), start.Add(2*pendingBandwidthTTL))
	if !applied || bytesIn > 1+100*uint64(pendingBandwidthTTL/time.Second) || bytesOut != 0 {
		t.Errorf("applied %d/%d bytes, want at most %d/0", bytesIn, bytesOut, 1+100*uint64(pendingBandwidthTTL/time.Second))
	}
}

// TestPendingBandwidthLimit checks that the held updates do not grow beyond
// the limit.
func TestPendingBandwidthLimit(t *testing.T) {
	t.Parallel()

	p := newPendingBandwidth()
	start := time.Unix(1_000_000, 0)

	for i := range pendingBandwidthMaxEntries + 100 {
		deliverBandwidth(t, p, nil, additiveUpdate(fmt.Sprintf("conn-%d", i), 100, 10), start)
	}
	if len(p.entries) != pendingBandwidthMaxEntries {
		t.Errorf("%d entries are held, want %d", len(p.entries), pendingBandwidthMaxEntries)
	}

	// Held connections still accumulate, the dropped ones were not held.
	deliverBandwidth(t, p, nil, additiveUpdate("conn-0", 100, 10), start)
	conn := &network.Connection{}
	bytesIn, bytesOut, applied := deliverBandwidth(t, p, conn, additiveUpdate("conn-0", 0, 0), start)
	expectApplied(t, bytesIn, bytesOut, applied, 200, 20)
	conn = &network.Connection{}
	bytesIn, bytesOut, applied = deliverBandwidth(t, p, conn, additiveUpdate(fmt.Sprintf("conn-%d", pendingBandwidthMaxEntries+50), 1, 1), start)
	expectApplied(t, bytesIn, bytesOut, applied, 1, 1)

	// Expired entries make room.
	deliverBandwidth(t, p, nil, additiveUpdate("new", 1, 1), start.Add(pendingBandwidthTTL+time.Second))
	if len(p.entries) != 1 {
		t.Errorf("%d entries are held, want 1", len(p.entries))
	}
}

// TestPendingBandwidthAbsolute checks that absolute updates are not held.
func TestPendingBandwidthAbsolute(t *testing.T) {
	t.Parallel()

	p := newPendingBandwidth()
	start := time.Unix(1_000_000, 0)
	absolute := &packet.BandwidthUpdate{
		ConnID:        testConnID,
		BytesReceived: 1000,
		BytesSent:     100,
		Method:        packet.Absolute,
	}

	if p.resolve(absolute, false, start) != nil {
		t.Error("update of an unknown connection must not be returned")
	}
	if len(p.entries) != 0 {
		t.Errorf("%d entries are held, want 0", len(p.entries))
	}
	if p.resolve(absolute, true, start) != absolute {
		t.Error("absolute update was changed")
	}

	// Held additive bytes are not added to absolute updates.
	p.resolve(additiveUpdate(testConnID, 1, 1), false, start)
	if got := p.resolve(absolute, true, start); got != absolute {
		t.Errorf("absolute update was changed: %s", got)
	}
}

// TestPendingBandwidthLockedConnection checks that held bytes that are added
// to an update for a locked connection are deferred with the update and
// credited once.
func TestPendingBandwidthLockedConnection(t *testing.T) {
	t.Parallel()

	p := newPendingBandwidth()
	conn := &network.Connection{}
	start := time.Unix(1_000_000, 0)

	deliverBandwidth(t, p, nil, additiveUpdate(testConnID, 100, 10), start)

	// The connection is found, but locked: The update is deferred.
	conn.Lock()
	if _, _, applied := deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 5, 1), start.Add(time.Second)); applied {
		t.Fatal("update must not be applied to a locked connection")
	}
	conn.Unlock()
	if len(p.entries) != 0 {
		t.Errorf("%d entries are still held", len(p.entries))
	}

	// The next update includes the deferred and the held bytes, once.
	bytesIn, bytesOut, applied := deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 1, 1), start.Add(2*time.Second))
	expectApplied(t, bytesIn, bytesOut, applied, 106, 12)
	bytesIn, bytesOut, applied = deliverBandwidth(t, p, conn, additiveUpdate(testConnID, 1, 1), start.Add(3*time.Second))
	expectApplied(t, bytesIn, bytesOut, applied, 1, 1)
	if conn.BytesReceived != 107 || conn.BytesSent != 13 {
		t.Errorf("connection has %d/%d bytes, want 107/13", conn.BytesReceived, conn.BytesSent)
	}
}
