package network

import (
	"testing"
	"time"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/state"
)

func TestIPConnActive(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recently := now.Add(-time.Minute).Unix()
	longAgo := now.Add(-state.UDPConnectionTTL - time.Minute).Unix()

	tests := []struct {
		name             string
		protocol         packet.IPProtocol
		existsInState    bool
		verdictPermanent bool
		lastSeen         int64
		socketExists     bool
		want             bool
	}{
		{"TCP in state", packet.TCP, true, false, longAgo, true, true},
		{"TCP not in state", packet.TCP, false, false, recently, true, false},
		{"UDP in state", packet.UDP, true, false, longAgo, true, true},
		{"UDP not in state, inactive", packet.UDP, false, false, longAgo, true, false},
		// The UDP state only knows about the last lookup of the connection,
		// which is only done for its first packet. Long-lived UDP flows that
		// are still sending packets must not be ended.
		{"UDP not in state, packets seen recently", packet.UDP, false, false, recently, true, true},
		// Packets for a closed socket do not keep the connection active.
		{"UDP not in state, packets seen recently, socket closed", packet.UDP, false, false, recently, false, false},
		// The last seen timestamp of permanent verdict connections is set by
		// the cleaner itself while the connection is in the state, as their
		// packets are not seen. Using it would keep them active for another
		// TTL after they left the state.
		{"UDP not in state, permanent verdict", packet.UDP, false, true, recently, true, false},
	}
	for _, tc := range tests {
		got := ipConnActive(tc.protocol, tc.existsInState, tc.verdictPermanent, tc.lastSeen, now, func() bool {
			return tc.socketExists
		})
		if got != tc.want {
			t.Errorf("%s: ipConnActive() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCleanerKeepsActiveUDPFlow simulates the cleaner runs of a long-lived
// UDP flow, whose packets are handled by Portmaster, but which is only looked
// up for its first packet.
func TestCleanerKeepsActiveUDPFlow(t *testing.T) {
	t.Parallel()

	started := time.Now()
	lastSeen := started.Unix()
	for elapsed := time.Duration(0); elapsed <= time.Hour; elapsed += cleanerTickDuration {
		now := started.Add(elapsed)
		// The flow sends a packet every second.
		lastSeen = now.Add(-time.Second).Unix()
		// The UDP state only has the time of the lookup of the first packet.
		existsInState := now.Sub(started) < state.UDPConnectionTTL
		if !ipConnActive(packet.UDP, existsInState, false, lastSeen, now, func() bool { return true }) {
			t.Fatalf("active UDP flow marked as ended after %s", elapsed)
		}
	}

	// The flow stops sending packets: it must end after the TTL.
	stopped := started.Add(time.Hour)
	var endedAfter time.Duration
	for elapsed := time.Duration(0); elapsed <= 2*state.UDPConnectionTTL; elapsed += cleanerTickDuration {
		if !ipConnActive(packet.UDP, false, false, lastSeen, stopped.Add(elapsed), func() bool { return true }) {
			endedAfter = elapsed
			break
		}
	}
	if endedAfter == 0 || endedAfter > state.UDPConnectionTTL+cleanerTickDuration {
		t.Fatalf("inactive UDP flow ended after %s, want about %s", endedAfter, state.UDPConnectionTTL)
	}
}

// TestCleanerEndsPermanentVerdictUDPFlow simulates the cleaner runs of a UDP
// flow with a permanent verdict, whose packets are not handled by Portmaster.
// The cleaner sets its last seen timestamp while it exists in the state, which
// must not keep it active after it left the state.
func TestCleanerEndsPermanentVerdictUDPFlow(t *testing.T) {
	t.Parallel()

	started := time.Now()
	lastSeen := started.Unix()
	for elapsed := time.Duration(0); elapsed <= 2*state.UDPConnectionTTL; elapsed += cleanerTickDuration {
		now := started.Add(elapsed)
		existsInState := now.Sub(started) < state.UDPConnectionTTL
		// Same as in cleanConnections().
		if existsInState {
			lastSeen = now.Unix()
		}
		if !ipConnActive(packet.UDP, existsInState, true, lastSeen, now, func() bool { return true }) {
			if elapsed != state.UDPConnectionTTL {
				t.Fatalf("permanent verdict UDP flow ended after %s, want %s", elapsed, state.UDPConnectionTTL)
			}
			return
		}
	}
	t.Fatal("permanent verdict UDP flow was never ended")
}
