package state

import (
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

// newTestTCPTable returns a TCP table that serves the given connections
// instead of reading them from the system.
func newTestTCPTable(version int, connections []*socket.ConnectionInfo) *tcpTable {
	table := &tcpTable{
		version:      version,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable: func() ([]*socket.ConnectionInfo, []*socket.BindInfo, error) {
			return connections, nil, nil
		},
	}
	table.updateTables()
	return table
}

// cleanerPacket returns a packet info as built by the connection cleaner
// (service/network/clean.go) to check if a connection still exists.
func cleanerPacket(version packet.IPVersion, protocol packet.IPProtocol, local net.IP, localPort uint16, remote net.IP, remotePort uint16) *packet.Info {
	return &packet.Info{
		Inbound:  false, // src == local
		Version:  version,
		Protocol: protocol,
		Src:      local,
		SrcPort:  localPort,
		Dst:      remote,
		DstPort:  remotePort,
		PID:      socket.UndefinedProcessID,
		// The cleaner uses the connection start time, so tables built by the
		// test helpers are recent enough and are not updated again.
		SeenAt: time.Now().Add(-time.Minute),
	}
}

var (
	testLocal4  = net.IPv4(192, 168, 1, 10).To4()
	testRemote4 = net.IPv4(9, 9, 9, 9).To4()
	testLocal6  = net.ParseIP("2001:db8::10")
	testRemote6 = net.ParseIP("2001:db8::99")
)

// v4Mapped returns the IPv4-mapped IPv6 address of the given IPv4 address, as
// it is read from /proc/net/{tcp,udp}6 for IPv4 traffic of dual-stack sockets.
func v4Mapped(ip net.IP) net.IP {
	mapped := make(net.IP, net.IPv6len)
	copy(mapped, net.IPv4(0, 0, 0, 0))
	copy(mapped[12:], ip.To4())
	return mapped
}

func TestTCPExists(t *testing.T) {
	t.Parallel()

	// IPv4 connection on a dual-stack (AF_INET6) socket: only listed in the
	// TCP6 table, with IPv4-mapped addresses.
	dualStackConn := &socket.ConnectionInfo{
		Local:  socket.Address{IP: v4Mapped(testLocal4), Port: 40000},
		Remote: socket.Address{IP: v4Mapped(testRemote4), Port: 443},
	}
	// Plain IPv4 connection, with addresses in 16 byte form as on Linux.
	plain4Conn := &socket.ConnectionInfo{
		Local:  socket.Address{IP: net.IPv4(192, 168, 1, 10), Port: 40001},
		Remote: socket.Address{IP: net.IPv4(9, 9, 9, 9), Port: 443},
	}
	plain6Conn := &socket.ConnectionInfo{
		Local:  socket.Address{IP: testLocal6, Port: 40002},
		Remote: socket.Address{IP: testRemote6, Port: 443},
	}

	tcp6 := newTestTCPTable(6, []*socket.ConnectionInfo{dualStackConn, plain6Conn})
	tcp4 := newTestTCPTable(4, []*socket.ConnectionInfo{plain4Conn})
	tcp4.dualStack = tcp6
	tcp4NoDualStack := newTestTCPTable(4, []*socket.ConnectionInfo{plain4Conn})

	tests := []struct {
		name  string
		table *tcpTable
		pkt   *packet.Info
		want  bool
	}{
		{
			name:  "IPv4 on dual-stack socket",
			table: tcp4,
			pkt:   cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40000, testRemote4, 443),
			want:  true,
		},
		{
			name:  "IPv4 on dual-stack socket, dual-stack disabled",
			table: tcp4NoDualStack,
			pkt:   cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40000, testRemote4, 443),
			want:  false,
		},
		{
			name:  "IPv4",
			table: tcp4,
			pkt:   cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40001, testRemote4, 443),
			want:  true,
		},
		{
			name:  "IPv4, dual-stack disabled",
			table: tcp4NoDualStack,
			pkt:   cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40001, testRemote4, 443),
			want:  true,
		},
		{
			name:  "IPv4 closed",
			table: tcp4,
			pkt:   cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40003, testRemote4, 443),
			want:  false,
		},
		{
			name:  "IPv4 other remote port",
			table: tcp4,
			pkt:   cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40000, testRemote4, 80),
			want:  false,
		},
		{
			name:  "IPv6",
			table: tcp6,
			pkt:   cleanerPacket(packet.IPv6, packet.TCP, testLocal6, 40002, testRemote6, 443),
			want:  true,
		},
		{
			name:  "IPv6 closed",
			table: tcp6,
			pkt:   cleanerPacket(packet.IPv6, packet.TCP, testLocal6, 40003, testRemote6, 443),
			want:  false,
		},
	}
	for _, tc := range tests {
		if got := tc.table.exists(tc.pkt, time.Now()); got != tc.want {
			t.Errorf("%s: exists() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestUDPExistsDualStack(t *testing.T) {
	t.Parallel()

	udp4, udp6, _ := newDualStackTestTables()
	now := time.Now()

	// Outbound IPv4 flow from the dual-stack [::]:5353 socket.
	pkt := udp4TestPacket(false)
	_, _, err := udp4.lookup(pkt, true)
	if err != nil {
		t.Fatalf("lookup failed: %s", err)
	}

	check := cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 53)
	if !udp4.exists(check, now) {
		t.Fatal("IPv4 flow on open dual-stack socket reported as ended")
	}

	// Flow that was never seen.
	if udp4.exists(cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 54), now) {
		t.Fatal("unseen flow on dual-stack socket reported as existing")
	}

	// Flow that was not seen within the TTL.
	if udp4.exists(check, now.Add(UDPConnectionTTL+time.Minute)) {
		t.Fatal("expired flow on dual-stack socket reported as existing")
	}

	// Close the dual-stack socket.
	udp6.lock.Lock()
	udp6.binds = nil
	udp6.lock.Unlock()
	if udp4.exists(check, now) {
		t.Fatal("flow on closed dual-stack socket reported as existing")
	}
}

// TestUDPSocketExistsAfterFlowExpired checks the socket check that the
// cleaner uses for UDP flows whose packets are still seen: Exists() only knows
// the time the flow was last looked up, but the socket is still bound.
func TestUDPSocketExistsAfterFlowExpired(t *testing.T) {
	t.Parallel()

	udp4, udp6, _ := newDualStackTestTables()
	now := time.Now()

	// Outbound IPv4 flow from the dual-stack [::]:5353 socket, looked up for
	// its first packet only.
	_, _, err := udp4.lookup(udp4TestPacket(false), true)
	if err != nil {
		t.Fatalf("lookup failed: %s", err)
	}
	check := cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 53)
	later := now.Add(UDPConnectionTTL + time.Minute)

	if udp4.exists(check, later) {
		t.Fatal("flow not looked up within the TTL reported as existing")
	}
	if udp4.findBindOrDualStack(check) == nil {
		t.Fatal("bound dual-stack socket of the flow not found")
	}
	if udp4.findBindOrDualStack(cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5354, testRemote4, 53)) != nil {
		t.Fatal("socket found for a port that is not bound")
	}

	// Close the dual-stack socket.
	udp6.lock.Lock()
	udp6.binds = nil
	udp6.lock.Unlock()
	if udp4.findBindOrDualStack(check) != nil {
		t.Fatal("closed dual-stack socket of the flow found")
	}
}

func TestUDPExists(t *testing.T) {
	t.Parallel()

	// IPv4 binds in 16 byte form, as on Linux: two binds on the same port.
	udp4 := newTestUDPTable(4, []*socket.BindInfo{
		{Local: socket.Address{IP: net.IPv4(127, 0, 0, 1), Port: 5353}, PID: 1},
		{Local: socket.Address{IP: net.IPv4(192, 168, 1, 10), Port: 5353}, PID: 2},
	})
	udp6 := newTestUDPTable(6, []*socket.BindInfo{
		{Local: socket.Address{IP: testLocal6, Port: 5353}, PID: 3},
	})
	now := time.Now()

	// IPv4
	_, _, err := udp4.lookup(udp4TestPacket(false), true)
	if err != nil {
		t.Fatalf("IPv4 lookup failed: %s", err)
	}
	if !udp4.exists(cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 53), now) {
		t.Error("open IPv4 flow reported as ended")
	}
	if udp4.exists(cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 54), now) {
		t.Error("unseen IPv4 flow reported as existing")
	}
	if udp4.exists(cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5354, testRemote4, 53), now) {
		t.Error("IPv4 flow without socket reported as existing")
	}

	// IPv6
	pkt6 := &packet.Info{
		Version:  packet.IPv6,
		Protocol: packet.UDP,
		Src:      testLocal6,
		SrcPort:  5353,
		Dst:      testRemote6,
		DstPort:  53,
		SeenAt:   time.Now(),
	}
	_, _, err = udp6.lookup(pkt6, true)
	if err != nil {
		t.Fatalf("IPv6 lookup failed: %s", err)
	}
	if !udp6.exists(cleanerPacket(packet.IPv6, packet.UDP, testLocal6, 5353, testRemote6, 53), now) {
		t.Error("open IPv6 flow reported as ended")
	}
	if udp6.exists(cleanerPacket(packet.IPv6, packet.UDP, testLocal6, 5353, testRemote6, 54), now) {
		t.Error("unseen IPv6 flow reported as existing")
	}
	if udp6.exists(cleanerPacket(packet.IPv6, packet.UDP, testLocal6, 5353, testRemote6, 53), now.Add(UDPConnectionTTL+time.Minute)) {
		t.Error("expired IPv6 flow reported as existing")
	}
}

func TestUDPExistsConcurrentWithLookup(t *testing.T) {
	t.Parallel()

	udp4 := newTestUDPTable(4, []*socket.BindInfo{
		{Local: socket.Address{IP: net.IPv4(192, 168, 1, 10), Port: 5353}, PID: 1},
	})
	_, _, err := udp4.lookup(udp4TestPacket(false), true)
	if err != nil {
		t.Fatalf("lookup failed: %s", err)
	}
	check := cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 53)

	// Lookups update the last seen time of the state, while the cleaner
	// reads it. Run with -race.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			_, _, _ = udp4.lookup(udp4TestPacket(true), true)
		}
	}()
	for range 100 {
		if !udp4.exists(check, time.Now()) {
			t.Error("open flow reported as ended")
			break
		}
	}
	<-done
}

func BenchmarkTCPExistsDualStack(b *testing.B) {
	connections := make([]*socket.ConnectionInfo, 0, 100)
	for i := range uint16(100) {
		connections = append(connections, &socket.ConnectionInfo{
			Local:  socket.Address{IP: v4Mapped(testLocal4), Port: 40000 + i},
			Remote: socket.Address{IP: v4Mapped(testRemote4), Port: 443},
		})
	}
	tcp4 := newTestTCPTable(4, nil)
	tcp4.dualStack = newTestTCPTable(6, connections)
	pkt := cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40099, testRemote4, 443)
	now := time.Now()

	b.ReportAllocs()
	for b.Loop() {
		if !tcp4.exists(pkt, now) {
			b.Fatal("not found")
		}
	}
}

func BenchmarkUDPExistsDualStack(b *testing.B) {
	udp4 := newBenchUDPTables()
	_, _, err := udp4.lookup(udp4TestPacket(false), true)
	if err != nil {
		b.Fatal(err)
	}
	pkt := cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, 53)
	now := time.Now()

	b.ReportAllocs()
	for b.Loop() {
		if !udp4.exists(pkt, now) {
			b.Fatal("not found")
		}
	}
}
