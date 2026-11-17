package state

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

var errTestFetchFailed = errors.New("test: fetching the socket table failed")

// testCleanerChecks is the number of Exists() checks of one cleaner run in
// the tests, as done by the cleaner for that many open connections.
const testCleanerChecks = 200

// newFailingTestTCPTable returns a TCP table that serves the given connections
// until failing is set, and counts the table fetches.
func newFailingTestTCPTable(connections []*socket.ConnectionInfo) (table *tcpTable, fetches *atomic.Int64, failing *atomic.Bool) {
	fetches = new(atomic.Int64)
	failing = new(atomic.Bool)
	table = &tcpTable{
		version:      4,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable: func() ([]*socket.ConnectionInfo, []*socket.BindInfo, error) {
			fetches.Add(1)
			if failing.Load() {
				return nil, nil, errTestFetchFailed
			}
			return connections, nil, nil
		},
	}
	table.updateTables()
	return table, fetches, failing
}

// newFailingTestUDPTable returns a UDP table that serves the given binds
// until failing is set, and counts the table fetches.
func newFailingTestUDPTable(binds []*socket.BindInfo) (table *udpTable, fetches *atomic.Int64, failing *atomic.Bool) {
	fetches = new(atomic.Int64)
	failing = new(atomic.Bool)
	table = &udpTable{
		version:      4,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable: func() ([]*socket.BindInfo, error) {
			fetches.Add(1)
			if failing.Load() {
				return nil, errTestFetchFailed
			}
			return binds, nil
		},
		states: make(map[string]map[string]*udpState),
	}
	table.updateTables()
	return table, fetches, failing
}

func TestTCPExistsDoesNotRetryFailedFetchInCleanerRun(t *testing.T) {
	t.Parallel()

	tcp4, fetches, failing := newFailingTestTCPTable([]*socket.ConnectionInfo{{
		Local:  socket.Address{IP: testLocal4, Port: 40000},
		Remote: socket.Address{IP: testRemote4, Port: 443},
	}})
	pkt := cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40000, testRemote4, 443)

	// Next cleaner run: the tables are stale and fetching them fails.
	failing.Store(true)
	tick := waitForNextCleanerRun(tcp4)
	start := fetches.Load()
	began := time.Now()
	for range testCleanerChecks {
		// The connection is still reported from the last good table.
		if !tcp4.exists(pkt, tick) {
			t.Fatal("connection of the last good table reported as ended")
		}
	}
	took := time.Since(began)
	n := fetches.Load() - start
	t.Logf("%d checks with a failing fetch: %d fetches in %s", testCleanerChecks, n, took)
	if n != 1 {
		t.Errorf("TCP table fetched %d times in one cleaner run with a failing fetch, want 1", n)
	}

	// The next cleaner run tries again and picks up the recovered table.
	failing.Store(false)
	tick = waitForNextCleanerRun(tcp4)
	start = fetches.Load()
	if !tcp4.exists(pkt, tick) {
		t.Fatal("connection reported as ended after the fetch recovered")
	}
	if n := fetches.Load() - start; n != 1 {
		t.Errorf("TCP table fetched %d times in the cleaner run after the failure, want 1", n)
	}
}

func TestUDPExistsDoesNotRetryFailedFetchInCleanerRun(t *testing.T) {
	t.Parallel()

	udp4, fetches, failing := newFailingTestUDPTable([]*socket.BindInfo{{
		Local: socket.Address{IP: net.IPv4zero, Port: 5353},
		PID:   4242,
	}})
	// Make the table older than the connections, as when the connections
	// started after the last good fetch.
	udp4.lastUpdateAt.Add(-(2 * time.Minute).Nanoseconds())
	udp4.lastFetchAttemptAt.Add(-(2 * time.Minute).Nanoseconds())

	failing.Store(true)
	now := time.Now()
	start := fetches.Load()
	began := time.Now()
	for i := range testCleanerChecks {
		udp4.exists(cleanerPacket(packet.IPv4, packet.UDP, testLocal4, 5353, testRemote4, uint16(1000+i)), now) //nolint:gosec // Small test values.
	}
	took := time.Since(began)
	n := fetches.Load() - start
	t.Logf("%d checks with a failing fetch: %d fetches in %s", testCleanerChecks, n, took)
	if n != 1 {
		t.Errorf("UDP table fetched %d times in one cleaner run with a failing fetch, want 1", n)
	}
}

func TestLookupRetriesAfterFailedFetch(t *testing.T) {
	t.Parallel()

	// A connection is opened after the last good fetch.
	conn := &socket.ConnectionInfo{
		Local:  socket.Address{IP: testLocal4, Port: 40000},
		Remote: socket.Address{IP: testRemote4, Port: 443},
		PID:    4242, // Set, so that CheckPID does not search /proc.
	}
	var osTable atomic.Pointer[[]*socket.ConnectionInfo]
	osTable.Store(&[]*socket.ConnectionInfo{})
	failing := new(atomic.Bool)
	tcp4 := &tcpTable{
		version:      4,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable: func() ([]*socket.ConnectionInfo, []*socket.BindInfo, error) {
			if failing.Load() {
				return nil, nil, errTestFetchFailed
			}
			return *osTable.Load(), nil, nil
		},
	}
	tcp4.updateTables()

	pkt := cleanerPacket(packet.IPv4, packet.TCP, testLocal4, 40000, testRemote4, 443)
	pkt.SeenAt = time.Now()
	osTable.Store(&[]*socket.ConnectionInfo{conn})

	// The cleaner runs and the fetch fails.
	failing.Store(true)
	tcp4.exists(pkt, time.Now())
	failing.Store(false)

	// A fast lookup of the new connection must still fetch the table again.
	pid, _, err := tcp4.lookup(pkt, true)
	if err != nil {
		t.Fatalf("lookup after a failed fetch failed: %s", err)
	}
	if pid != 4242 {
		t.Fatalf("lookup after a failed fetch returned pid %d, want 4242", pid)
	}
}
