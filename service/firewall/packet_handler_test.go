package firewall

import (
	"net"
	"testing"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/access/account"
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

// TestResetSingleConnectionVerdictSkipsIncomplete checks that a connection
// whose data is not yet complete is not re-evaluated after a re-attribution
// event, as it has no process, entity or profile yet. Its verdict is decided
// by its firewall handler once its data is complete.
func TestResetSingleConnectionVerdictSkipsIncomplete(t *testing.T) { //nolint:paralleltest // Modifies the global user cache.
	// Without a database, the user lookup fails and the feature flags of the
	// connection are not updated. With the service running, the user is
	// either known or not logged in, which both let the feature flag update
	// access the connection data. Saving the user caches it, persisting it
	// fails without a database.
	_ = (&access.UserRecord{User: &account.User{View: &account.View{}}}).Save()

	conn := network.NewIncompleteConnection(realTestPacket{packet.NewInfoPacket(packet.Info{
		Inbound:  false,
		Version:  packet.IPv4,
		Protocol: packet.TCP,
		Src:      net.IPv4(127, 0, 0, 1),
		SrcPort:  42017,
		Dst:      net.IPv4(192, 0, 2, 17),
		DstPort:  443,
	})})

	// Connection IDs are reused, so the connection the re-attribution event
	// refers to may have been replaced by a new, incomplete one.
	resetSingleConnectionVerdict(conn.ID)

	conn.Lock()
	defer conn.Unlock()
	if conn.DataIsComplete() {
		t.Error("connection must stay incomplete")
	}
	if conn.Verdict != network.VerdictUndecided {
		t.Errorf("verdict of incomplete connection changed to %s", conn.VerdictVerb())
	}
}

// TestApplyBandwidthUpdateCounterReset checks that an absolute bandwidth update
// with lower counters than the connection has does not underflow, but is
// treated as a reset counter.
func TestApplyBandwidthUpdateCounterReset(t *testing.T) {
	t.Parallel()

	conn := &network.Connection{}
	apply := func(received, sent uint64) (bytesIn, bytesOut uint64) {
		t.Helper()

		bytesIn, bytesOut, ok := applyBandwidthUpdate(conn, &packet.BandwidthUpdate{
			BytesReceived: received,
			BytesSent:     sent,
			Method:        packet.Absolute,
		})
		if !ok {
			t.Fatal("update was not applied to an unlocked connection")
		}
		conn.Unlock()
		return bytesIn, bytesOut
	}

	if bytesIn, bytesOut := apply(1000, 500); bytesIn != 1000 || bytesOut != 500 {
		t.Errorf("reported %d/%d bytes, want 1000/500", bytesIn, bytesOut)
	}

	// The received counter was reset and counted 100 bytes since then.
	if bytesIn, bytesOut := apply(100, 600); bytesIn != 100 || bytesOut != 100 {
		t.Errorf("reported %d/%d bytes after counter reset, want 100/100", bytesIn, bytesOut)
	}
	if conn.BytesReceived != 100 || conn.BytesSent != 600 {
		t.Errorf("connection has %d/%d bytes, want 100/600", conn.BytesReceived, conn.BytesSent)
	}

	// The reset counter is the new baseline.
	if bytesIn, bytesOut := apply(150, 600); bytesIn != 50 || bytesOut != 0 {
		t.Errorf("reported %d/%d bytes, want 50/0", bytesIn, bytesOut)
	}
}
