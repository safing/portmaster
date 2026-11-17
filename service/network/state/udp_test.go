package state

import (
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

// newTestUDPTable returns a UDP table that serves the given binds instead of
// reading them from the system.
func newTestUDPTable(version int, binds []*socket.BindInfo) *udpTable {
	table := &udpTable{
		version:      version,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable: func() ([]*socket.BindInfo, error) {
			return binds, nil
		},
		states: make(map[string]map[string]*udpState),
	}
	table.updateTables()
	return table
}

// newDualStackTestTables models a host with IPv6 enabled: the UDP4 table has
// no sockets of its own and the UDP6 table, which is attached as the
// dual-stack table of the UDP4 table, holds a wildcard [::]:5353 socket that
// also receives IPv4 traffic.
func newDualStackTestTables() (udp4, udp6 *udpTable, dualStackBind *socket.BindInfo) {
	dualStackBind = &socket.BindInfo{
		Local: socket.Address{IP: net.IPv6zero, Port: 5353},
		PID:   4242, // Set, so that CheckPID does not search /proc.
	}
	udp6 = newTestUDPTable(6, []*socket.BindInfo{dualStackBind})
	udp4 = newTestUDPTable(4, nil)
	udp4.dualStack = udp6
	return udp4, udp6, dualStackBind
}

func udp4TestPacket(inbound bool) *packet.Info {
	local := net.IPv4(192, 168, 1, 10).To4()
	remote := net.IPv4(9, 9, 9, 9).To4()
	pkt := &packet.Info{
		Inbound:  inbound,
		Version:  packet.IPv4,
		Protocol: packet.UDP,
		SeenAt:   time.Now(),
	}
	if inbound {
		pkt.Src, pkt.SrcPort, pkt.Dst, pkt.DstPort = remote, 53, local, 5353
	} else {
		pkt.Src, pkt.SrcPort, pkt.Dst, pkt.DstPort = local, 5353, remote, 53
	}
	return pkt
}

func TestUDPDualStackDirectionSurvivesCleaning(t *testing.T) {
	t.Parallel()

	udp4, _, _ := newDualStackTestTables()

	// First packet of the flow is outbound.
	pid, inbound, err := udp4.lookup(udp4TestPacket(false), true)
	if err != nil {
		t.Fatalf("lookup of outbound packet failed: %s", err)
	}
	if pid != 4242 || inbound {
		t.Fatalf("unexpected lookup result for outbound packet: pid=%d inbound=%v", pid, inbound)
	}

	// The cleaner runs while the dual-stack socket is still open.
	udp4.cleanStates(time.Now().UTC())

	// The reply must still be attributed to the outbound flow.
	_, inbound, err = udp4.lookup(udp4TestPacket(true), true)
	if err != nil {
		t.Fatalf("lookup of reply packet failed: %s", err)
	}
	if inbound {
		t.Fatal("direction state of open dual-stack socket was cleaned: reply of outbound flow reported as inbound")
	}
}

func TestUDPDualStackStateRemovedWhenSocketClosed(t *testing.T) {
	t.Parallel()

	udp4, udp6, _ := newDualStackTestTables()

	_, _, err := udp4.lookup(udp4TestPacket(false), true)
	if err != nil {
		t.Fatalf("lookup failed: %s", err)
	}
	if len(udp4.states) != 1 {
		t.Fatalf("expected state for 1 socket, got %d", len(udp4.states))
	}

	// Close the dual-stack socket.
	udp6.lock.Lock()
	udp6.binds = nil
	udp6.lock.Unlock()

	udp4.cleanStates(time.Now().UTC())
	if len(udp4.states) != 0 {
		t.Fatalf("state of closed dual-stack socket was not removed: %d sockets left", len(udp4.states))
	}
}

func TestUDPStateTTL(t *testing.T) {
	t.Parallel()

	udp4, _, bind := newDualStackTestTables()
	now := time.Now().UTC()

	// Add more than AggressiveCleaningThreshold remotes, half of them older
	// than the shortened TTL, plus one older than the full TTL.
	remotes := make(map[string]*udpState)
	for i := range uint32(AggressiveCleaningThreshold + 10) {
		lastSeen := now
		if i%2 == 0 {
			lastSeen = now.Add(-UDPConnStateShortenedTTL - time.Minute)
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, 0x0a000000+i)
		remotes[makeUDPStateKey(socket.Address{IP: ip, Port: 53})] = &udpState{lastSeen: lastSeen}
	}
	remotes[makeUDPStateKey(socket.Address{IP: net.IPv4(10, 1, 0, 0).To4(), Port: 53})] = &udpState{lastSeen: now.Add(-UDPConnStateTTL - time.Minute)}
	udp4.states[makeUDPStateKey(bind.Local)] = remotes

	udp4.cleanStates(now)
	if got, want := len(remotes), (AggressiveCleaningThreshold+10)/2; got != want {
		t.Fatalf("expected %d remaining remotes, got %d", want, got)
	}
}

func TestMakeUDPStateKey(t *testing.T) {
	t.Parallel()

	for _, address := range []socket.Address{
		{IP: net.IPv4(192, 168, 1, 1), Port: 0},
		{IP: net.IPv4(192, 168, 1, 1).To4(), Port: 53},
		{IP: net.IPv6zero, Port: 5353},
		{IP: net.ParseIP("2001:db8::1"), Port: 65535},
		{IP: nil, Port: 1},
	} {
		// Expected format: raw IP bytes followed by the decimal port.
		want := string(address.IP) + strconv.Itoa(int(address.Port))
		got := makeUDPStateKey(address)
		if got != want {
			t.Errorf("makeUDPStateKey(%v) = %q, want %q", address, got, want)
		}
	}
}

// TestUDPDualStackLookupUsesOwnTableAge checks that the first try of a lookup
// decides by the age of the dual-stack table itself whether it has to be
// updated, and not by the age of the UDP4 table.
func TestUDPDualStackLookupUsesOwnTableAge(t *testing.T) {
	t.Parallel()

	udp4, udp6, _ := newDualStackTestTables()

	// Count the fetches of the dual-stack table and do not limit them.
	var fetches int
	binds := udp6.getCurrentTables()
	udp6.fetchLimiter = utils.NewCallLimiter2(0)
	udp6.fetchTable = func() ([]*socket.BindInfo, error) {
		fetches++
		return binds, nil
	}

	// The packet was seen after the dual-stack table was updated, but before
	// the UDP4 table was.
	pkt := udp4TestPacket(false)
	udp4.lastUpdateAt.Store(pkt.SeenAt.Add(time.Second).UnixNano())

	pid, _, err := udp4.lookup(pkt, true)
	if err != nil {
		t.Fatalf("lookup failed: %s", err)
	}
	if pid != 4242 {
		t.Fatalf("unexpected pid %d", pid)
	}
	if fetches != 0 {
		t.Fatalf("dual-stack table was fetched %d times, although it is newer than the packet", fetches)
	}
}
