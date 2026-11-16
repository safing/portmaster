package state

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

// testCleanerTick is the interval of the connection cleaner
// (service/network/clean.go), which calls Exists().
const testCleanerTick = 5 * time.Second

// newMutableTestTCPTable returns a TCP table that serves the connections
// stored in osTable instead of reading them from the system, and counts the
// table fetches.
func newMutableTestTCPTable(version int, osTable *atomic.Pointer[[]*socket.ConnectionInfo]) (*tcpTable, *atomic.Int64) {
	fetches := new(atomic.Int64)
	table := &tcpTable{
		version:      version,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable: func() ([]*socket.ConnectionInfo, []*socket.BindInfo, error) {
			fetches.Add(1)
			return *osTable.Load(), nil, nil
		},
	}
	table.updateTables()
	return table, fetches
}

// waitForNextCleanerRun simulates the time passing until the next run of the
// connection cleaner by making the tables older, and returns the now of the
// next run.
func waitForNextCleanerRun(tables ...*tcpTable) (now time.Time) {
	for _, table := range tables {
		table.lastUpdateAt.Add(-testCleanerTick.Nanoseconds())
	}
	return time.Now()
}

func TestTCPExistsNoticesClosedConnection(t *testing.T) {
	t.Parallel()

	// The connections started a minute ago, the tables were updated after
	// that (by a lookup) and no lookups happen anymore (quiet system).
	plain4Conn := &socket.ConnectionInfo{
		Local:  socket.Address{IP: testLocal4, Port: 40000},
		Remote: socket.Address{IP: testRemote4, Port: 443},
	}
	dualStackConn := &socket.ConnectionInfo{
		Local:  socket.Address{IP: v4Mapped(testLocal4), Port: 40001},
		Remote: socket.Address{IP: v4Mapped(testRemote4), Port: 443},
	}
	var os4, os6 atomic.Pointer[[]*socket.ConnectionInfo]
	os4.Store(&[]*socket.ConnectionInfo{plain4Conn})
	os6.Store(&[]*socket.ConnectionInfo{dualStackConn})
	tcp4, fetches4 := newMutableTestTCPTable(4, &os4)
	tcp6, fetches6 := newMutableTestTCPTable(6, &os6)
	tcp4.dualStack = tcp6

	plainPkt := cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40000, testRemote4, 443)
	dualStackPkt := cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40001, testRemote4, 443)
	closedPkt := cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40002, testRemote4, 443)

	// Next cleaner run: both connections are open.
	tick := waitForNextCleanerRun(tcp4, tcp6)
	if !tcp4.exists(plainPkt, tick) {
		t.Fatal("open IPv4 connection reported as ended")
	}
	if !tcp4.exists(dualStackPkt, tick) {
		t.Fatal("open IPv4 connection on dual-stack socket reported as ended")
	}

	// Both connections are closed in the OS.
	os4.Store(&[]*socket.ConnectionInfo{})
	os6.Store(&[]*socket.ConnectionInfo{})

	// Next cleaner run: both connections must be reported as ended, and the
	// tables must be updated once in the run, not once per check.
	tick = waitForNextCleanerRun(tcp4, tcp6)
	start4, start6 := fetches4.Load(), fetches6.Load()
	if tcp4.exists(plainPkt, tick) {
		t.Error("closed IPv4 connection still reported as existing")
	}
	if tcp4.exists(dualStackPkt, tick) {
		t.Error("closed IPv4 connection on dual-stack socket still reported as existing")
	}
	for range 100 {
		tcp4.exists(closedPkt, tick)
	}
	if n := fetches4.Load() - start4; n != 1 {
		t.Errorf("TCP4 table fetched %d times in one cleaner run, want 1", n)
	}
	if n := fetches6.Load() - start6; n != 1 {
		t.Errorf("TCP6 table fetched %d times in one cleaner run, want 1", n)
	}
}

// BenchmarkTableFetch measures one fetch of the system socket tables, as done
// by an update of a table.
func BenchmarkTableFetch(b *testing.B) {
	tcpFetchers := map[string]func() ([]*socket.ConnectionInfo, []*socket.BindInfo, error){
		"TCP4": getTCP4Table,
		"TCP6": getTCP6Table,
	}
	udpFetchers := map[string]func() ([]*socket.BindInfo, error){
		"UDP4": getUDP4Table,
		"UDP6": getUDP6Table,
	}

	for name, fetch := range tcpFetchers {
		b.Run(name, func(b *testing.B) {
			connections, listeners, err := fetch()
			if err != nil {
				b.Skipf("system table not available: %s", err)
			}
			b.ReportAllocs()
			for b.Loop() {
				_, _, _ = fetch()
			}
			b.ReportMetric(float64(len(connections)+len(listeners)), "sockets")
		})
	}
	for name, fetch := range udpFetchers {
		b.Run(name, func(b *testing.B) {
			binds, err := fetch()
			if err != nil {
				b.Skipf("system table not available: %s", err)
			}
			b.ReportAllocs()
			for b.Loop() {
				_, _ = fetch()
			}
			b.ReportMetric(float64(len(binds)), "sockets")
		})
	}
}
