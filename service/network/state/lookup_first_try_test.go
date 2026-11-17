package state

import (
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

// TestLookupFirstTryUsesTablesNewerThanPacket checks that a lookup does not
// fetch the tables from the system if the current tables hold the socket and
// were fetched after the packet was seen, eg. by the lookup of another packet
// of the same burst. Fetching waits for the pause between table updates.
func TestLookupFirstTryUsesTablesNewerThanPacket(t *testing.T) {
	t.Parallel()

	local := net.IPv4(192, 168, 1, 10).To4()
	remote := net.IPv4(9, 9, 9, 9).To4()
	// The packet was seen before the tables are fetched below.
	seenAt := time.Now().Add(-time.Millisecond)

	t.Run("udp", func(t *testing.T) {
		t.Parallel()

		binds := []*socket.BindInfo{{
			Local: socket.Address{IP: local, Port: 5353},
			PID:   4242, // Set, so that CheckPID does not search /proc.
		}}
		table := newTestUDPTable(4, binds)
		var fetches int
		table.fetchTable = func() ([]*socket.BindInfo, error) {
			fetches++
			return binds, nil
		}

		pkt := udp4TestPacket(false)
		pkt.SeenAt = seenAt
		pid, _, err := table.lookup(pkt, true)
		if err != nil || pid != 4242 {
			t.Fatalf("unexpected lookup result: pid=%d err=%v", pid, err)
		}
		if fetches != 0 {
			t.Errorf("table was fetched %d times, although it holds the socket and is newer than the packet", fetches)
		}
	})

	t.Run("tcp", func(t *testing.T) {
		t.Parallel()

		connections := []*socket.ConnectionInfo{{
			Local:  socket.Address{IP: local, Port: 51234},
			Remote: socket.Address{IP: remote, Port: 443},
			PID:    4242,
		}}
		table := newTestTCPTable(4, connections)
		var fetches int
		table.fetchTable = func() ([]*socket.ConnectionInfo, []*socket.BindInfo, error) {
			fetches++
			return connections, nil, nil
		}

		pid, _, err := table.lookup(&packet.Info{
			Version:  packet.IPv4,
			Protocol: packet.TCP,
			Src:      local,
			SrcPort:  51234,
			Dst:      remote,
			DstPort:  443,
			SeenAt:   seenAt,
		}, true)
		if err != nil || pid != 4242 {
			t.Fatalf("unexpected lookup result: pid=%d err=%v", pid, err)
		}
		if fetches != 0 {
			t.Errorf("table was fetched %d times, although it holds the socket and is newer than the packet", fetches)
		}
	})
}
