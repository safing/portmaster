package firewall

import (
	"net"
	"testing"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
)

// realTestPacket is a packet that is not info-only, but otherwise behaves like
// an info packet. It is enough for the fast-track checks, which never apply a
// verdict themselves.
type realTestPacket struct {
	*packet.InfoPacket
}

func (pkt realTestPacket) InfoOnly() bool { return false }

func TestFastTrackedLocalDNSKeepsKnownPID(t *testing.T) { //nolint:paralleltest // Modifies the global nameserver IP matcher.
	nameserverIP := net.IPv4(127, 0, 0, 17)

	// Set nameserver IP matcher directly, as SetNameserverIPMatcher can only be called once.
	prevMatcher, prevReady := nameserverIPMatcher, nameserverIPMatcherReady.IsSet()
	nameserverIPMatcher = func(ip net.IP) bool { return ip.Equal(nameserverIP) }
	nameserverIPMatcherReady.Set()
	t.Cleanup(func() {
		nameserverIPMatcher = prevMatcher
		nameserverIPMatcherReady.SetTo(prevReady)
	})

	newDNSPacket := func(srcPort uint16, pid int) packet.Packet {
		return realTestPacket{packet.NewInfoPacket(packet.Info{
			Inbound:  false,
			Version:  packet.IPv4,
			Protocol: packet.UDP,
			Src:      net.IPv4(127, 0, 0, 1),
			SrcPort:  srcPort,
			Dst:      nameserverIP,
			DstPort:  53,
			PID:      pid,
		})}
	}

	tests := []struct {
		name          string
		connPID       int
		pktPID        int
		wantPID       int
		wantPermanent bool
	}{
		{
			// Linux: nfqueue packets carry no PID, the connection got it from an eBPF PID hint.
			name:          "packet without PID keeps hinted PID",
			connPID:       1234,
			pktPID:        process.UndefinedProcessID,
			wantPID:       1234,
			wantPermanent: true,
		},
		{
			// Windows: the kext reports the PID with the packet.
			name:          "packet PID is applied",
			connPID:       process.UndefinedProcessID,
			pktPID:        4321,
			wantPID:       4321,
			wantPermanent: true,
		},
		{
			name:          "no PID known at all",
			connPID:       process.UndefinedProcessID,
			pktPID:        process.UndefinedProcessID,
			wantPID:       process.UndefinedProcessID,
			wantPermanent: false,
		},
	}

	for i, tc := range tests { //nolint:paralleltest // Subtests share the global state modified by the parent test.
		t.Run(tc.name, func(t *testing.T) { //nolint:paralleltest // Subtests share the global state modified by the parent test.
			pkt := newDNSPacket(uint16(41000+i), tc.pktPID) //nolint:gosec // i is a small test index.
			conn := &network.Connection{PID: tc.connPID}

			verdict, permanent := fastTrackedPermit(conn, pkt)
			if verdict != network.VerdictAccept {
				t.Fatalf("expected local DNS to be fast-track accepted, got %s", verdict)
			}
			if conn.PID != tc.wantPID {
				t.Errorf("expected connection PID %d, got %d", tc.wantPID, conn.PID)
			}
			if permanent != tc.wantPermanent {
				t.Errorf("expected permanent=%v, got %v", tc.wantPermanent, permanent)
			}

			// The DNS request connection must be saved for fast DNS request
			// attribution whenever the PID is known.
			_, saved := network.GetDNSRequestConnection(pkt.Info())
			if wantSaved := tc.wantPID != process.UndefinedProcessID; saved != wantSaved {
				t.Errorf("expected dns request connection saved=%v, got %v", wantSaved, saved)
			}
		})
	}
}

// TestApplyBandwidthUpdateWhileLocked checks that additive bandwidth updates
// are not lost when the connection is locked while they arrive.
func TestApplyBandwidthUpdateWhileLocked(t *testing.T) {
	t.Parallel()

	conn := &network.Connection{}

	// The connection is locked, eg. while it is evaluated.
	conn.Lock()
	for range 3 {
		_, _, ok := applyBandwidthUpdate(conn, &packet.BandwidthUpdate{
			BytesReceived: 100,
			BytesSent:     50,
			Method:        packet.Additive,
		})
		if ok {
			t.Fatal("update must not be applied to a locked connection")
		}
	}
	conn.Unlock()

	// The next update includes the bytes of the skipped updates.
	bytesIn, bytesOut, ok := applyBandwidthUpdate(conn, &packet.BandwidthUpdate{
		BytesReceived: 10,
		BytesSent:     5,
		Method:        packet.Additive,
	})
	if !ok {
		t.Fatal("update was not applied to an unlocked connection")
	}
	if bytesIn != 310 || bytesOut != 155 {
		t.Errorf("reported %d/%d bytes, want 310/155", bytesIn, bytesOut)
	}
	if conn.BytesReceived != 310 || conn.BytesSent != 155 {
		t.Errorf("connection has %d/%d bytes, want 310/155", conn.BytesReceived, conn.BytesSent)
	}
	conn.Unlock()

	// Skipped absolute updates are included in the next one anyway.
	conn.Lock()
	if _, _, ok := applyBandwidthUpdate(conn, &packet.BandwidthUpdate{
		BytesReceived: 1000,
		BytesSent:     500,
		Method:        packet.Absolute,
	}); ok {
		t.Fatal("update must not be applied to a locked connection")
	}
	conn.Unlock()
	bytesIn, bytesOut, ok = applyBandwidthUpdate(conn, &packet.BandwidthUpdate{
		BytesReceived: 2000,
		BytesSent:     1000,
		Method:        packet.Absolute,
	})
	if !ok {
		t.Fatal("update was not applied to an unlocked connection")
	}
	if bytesIn != 1690 || bytesOut != 845 {
		t.Errorf("reported %d/%d bytes, want 1690/845", bytesIn, bytesOut)
	}
	if conn.BytesReceived != 2000 || conn.BytesSent != 1000 {
		t.Errorf("connection has %d/%d bytes, want 2000/1000", conn.BytesReceived, conn.BytesSent)
	}
	conn.Unlock()
}
