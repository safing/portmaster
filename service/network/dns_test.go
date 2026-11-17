package network

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/service/network/packet"
)

func newTestDNSRequestPacket(srcPort uint16) packet.Packet {
	pkt := packet.NewInfoPacket(packet.Info{
		Inbound:  false,
		Version:  packet.IPv4,
		Protocol: packet.UDP,
		Src:      net.IPv4(127, 0, 0, 1),
		SrcPort:  srcPort,
		Dst:      net.IPv4(127, 0, 0, 53),
		DstPort:  53,
		PID:      1234,
	})
	pkt.SetCtx(context.Background())
	return pkt
}

func resetDNSRequestConnections(t *testing.T) {
	t.Helper()

	dnsRequestConnectionsLock.Lock()
	dnsRequestConnections = make(map[string]*dnsRequestConnection)
	dnsRequestConnectionsLock.Unlock()
}

func dnsRequestConnectionsLen() int {
	dnsRequestConnectionsLock.RLock()
	defer dnsRequestConnectionsLock.RUnlock()

	return len(dnsRequestConnections)
}

// TestDNSRequestConnectionNeverEndedExpires covers DNS request connections
// that are saved by the DNS fast-track and then dropped from the connection
// state while still incomplete: their Ended field is never set.
func TestDNSRequestConnectionNeverEndedExpires(t *testing.T) { //nolint:paralleltest // Uses global state.
	resetDNSRequestConnections(t)
	t.Cleanup(func() { resetDNSRequestConnections(t) })

	now := time.Now()
	const entries = 1000
	for i := range entries {
		conn := &Connection{PID: 1234, Started: now.Unix()}
		SaveDNSRequestConnection(conn, newTestDNSRequestPacket(uint16(10000+i)))
	}
	if l := dnsRequestConnectionsLen(); l != entries {
		t.Fatalf("expected %d saved entries, got %d", entries, l)
	}

	// Fresh entries must be kept.
	cleanDNSRequestConnectionsAt(now)
	if l := dnsRequestConnectionsLen(); l != entries {
		t.Fatalf("fresh entries were removed: expected %d entries, got %d", entries, l)
	}

	// Unused entries of connections that never ended must expire.
	cleanDNSRequestConnectionsAt(now.Add(dnsRequestConnectionIdleTTL + time.Minute))
	if l := dnsRequestConnectionsLen(); l != 0 {
		t.Fatalf("expected all unused never-ended entries to expire, %d entries left", l)
	}
}

func TestDNSRequestConnectionKeptWhileUsed(t *testing.T) { //nolint:paralleltest // Uses global state.
	resetDNSRequestConnections(t)
	t.Cleanup(func() { resetDNSRequestConnections(t) })

	pkt := newTestDNSRequestPacket(20000)
	conn := &Connection{PID: 1234, Started: time.Now().Add(-time.Hour).Unix()}
	SaveDNSRequestConnection(conn, pkt)

	// Simulate that the entry was saved a long time ago.
	dnsRequestConnectionsLock.RLock()
	for _, entry := range dnsRequestConnections {
		entry.lastUsed.Store(time.Now().Add(-time.Hour).Unix())
	}
	dnsRequestConnectionsLock.RUnlock()

	// A lookup marks the entry as used.
	found, ok := GetDNSRequestConnection(pkt.Info())
	if !ok || found != conn {
		t.Fatal("expected to find saved dns request connection")
	}

	cleanDNSRequestConnectionsAt(time.Now())
	if _, ok := GetDNSRequestConnection(pkt.Info()); !ok {
		t.Fatal("recently used dns request connection was removed")
	}
}

func TestDNSRequestConnectionEndedExpires(t *testing.T) { //nolint:paralleltest // Uses global state.
	resetDNSRequestConnections(t)
	t.Cleanup(func() { resetDNSRequestConnections(t) })

	now := time.Now()

	ended := &Connection{PID: 1234, Started: now.Unix() - 10, Ended: now.Unix() - 5}
	SaveDNSRequestConnection(ended, newTestDNSRequestPacket(30000))
	justEnded := &Connection{PID: 1234, Started: now.Unix() - 10, Ended: now.Unix()}
	justEndedPkt := newTestDNSRequestPacket(30001)
	SaveDNSRequestConnection(justEnded, justEndedPkt)

	cleanDNSRequestConnectionsAt(now)
	if l := dnsRequestConnectionsLen(); l != 1 {
		t.Fatalf("expected 1 entry left, got %d", l)
	}
	if found, ok := GetDNSRequestConnection(justEndedPkt.Info()); !ok || found != justEnded {
		t.Fatal("just ended dns request connection was removed")
	}
}
