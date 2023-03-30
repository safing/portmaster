package state

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/socket"
)

const maxUpdateTries = 100

type tcpTable struct {
	version int

	connections []*socket.ConnectionInfo
	listeners   []*socket.BindInfo
	updateIter  atomic.Uint64
	lock        sync.RWMutex

	fetchingLock       sync.Mutex
	fetchingInProgress bool
	fetchingDoneSignal chan struct{}
	fetchTable         func() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error)

	dualStack *tcpTable
}

var (
	tcp6Table = &tcpTable{
		version:            6,
		fetchingDoneSignal: make(chan struct{}),
		fetchTable:         getTCP6Table,
	}

	tcp4Table = &tcpTable{
		version:            4,
		fetchingDoneSignal: make(chan struct{}),
		fetchTable:         getTCP4Table,
	}
)

// EnableTCPDualStack adds the TCP6 table to the TCP4 table as a dual-stack.
// Must be called before any lookup operation.
func EnableTCPDualStack() {
	tcp4Table.dualStack = tcp6Table
}

func (table *tcpTable) getCurrentTables() (
	connections []*socket.ConnectionInfo,
	listeners []*socket.BindInfo,
	updateIter uint64,
) {
	table.lock.RLock()
	defer table.lock.RUnlock()

	return table.connections, table.listeners, table.updateIter.Load()
}

func (table *tcpTable) checkFetchingState() (fetch bool, signal chan struct{}) {
	table.fetchingLock.Lock()
	defer table.fetchingLock.Unlock()

	// If fetching is already in progress, just return the signal.
	if table.fetchingInProgress {
		return false, table.fetchingDoneSignal
	}

	// Otherwise, tell caller to fetch.
	table.fetchingInProgress = true
	return true, nil
}

func (table *tcpTable) signalFetchComplete() {
	table.fetchingLock.Lock()
	defer table.fetchingLock.Unlock()

	// Set fetching state.
	table.fetchingInProgress = false

	// Signal waiting goroutines.
	close(table.fetchingDoneSignal)
	table.fetchingDoneSignal = make(chan struct{})
}

func (table *tcpTable) updateTables(previousUpdateIter uint64) (
	connections []*socket.ConnectionInfo,
	listeners []*socket.BindInfo,
	updateIter uint64,
) {
	var tries int

	// Attempt to update the tables until we get a new version of the tables.
	for previousUpdateIter == table.updateIter.Load() {
		// Abort if it takes too long.
		tries++
		if tries > maxUpdateTries {
			log.Warningf("state: failed to upate TCP%d socket table %d times", table.version, tries-1)
			return table.getCurrentTables()
		}

		// Check if someone is fetching or if we should fetch.
		fetch, signal := table.checkFetchingState()
		if fetch {
			defer table.signalFetchComplete()

			// Just to be sure, check again if there is a new version.
			if previousUpdateIter < table.updateIter.Load() {
				return table.getCurrentTables()
			}

			// Wait for 5 milliseconds.
			time.Sleep(5 * time.Millisecond)

			// Fetch new tables from system.
			connections, listeners, err := table.fetchTable()
			if err != nil {
				log.Warningf("state: failed to get TCP%d socket table: %s", table.version, err)
				// Return the current tables as fallback, as we need to trigger the defer to complete the fetch.
				return table.getCurrentTables()
			}

			// Pre-check for any listeners.
			for _, bindInfo := range listeners {
				bindInfo.ListensAny = bindInfo.Local.IP.Equal(net.IPv4zero) || bindInfo.Local.IP.Equal(net.IPv6zero)
			}

			// Apply new tables.
			table.lock.Lock()
			defer table.lock.Unlock()
			table.connections = connections
			table.listeners = listeners
			table.updateIter.Add(1)

			// Return new tables immediately.
			return table.connections, table.listeners, table.updateIter.Load()
		}

		// Otherwise, wait for fetch to complete.
		<-signal
	}

	return table.getCurrentTables()
}
