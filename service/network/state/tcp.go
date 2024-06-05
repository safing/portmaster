package state

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/network/socket"
)

const (
	minDurationBetweenTableUpdates = 10 * time.Millisecond
)

type tcpTable struct {
	version int

	connections []*socket.ConnectionInfo
	listeners   []*socket.BindInfo
	lock        sync.RWMutex

	// lastUpdateAt stores the time when the tables where last updated as unix nanoseconds.
	lastUpdateAt atomic.Int64

	fetchLimiter *utils.CallLimiter
	fetchTable   func() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error)

	dualStack *tcpTable
}

var (
	tcp6Table = &tcpTable{
		version:      6,
		fetchLimiter: utils.NewCallLimiter(minDurationBetweenTableUpdates),
		fetchTable:   getTCP6Table,
	}

	tcp4Table = &tcpTable{
		version:      4,
		fetchLimiter: utils.NewCallLimiter(minDurationBetweenTableUpdates),
		fetchTable:   getTCP4Table,
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
) {
	table.lock.RLock()
	defer table.lock.RUnlock()

	return table.connections, table.listeners
}

func (table *tcpTable) updateTables() (
	connections []*socket.ConnectionInfo,
	listeners []*socket.BindInfo,
) {
	// Fetch tables.
	table.fetchLimiter.Do(func() {
		// Fetch new tables from system.
		connections, listeners, err := table.fetchTable()
		if err != nil {
			log.Warningf("state: failed to get TCP%d socket table: %s", table.version, err)
			return
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
		table.lastUpdateAt.Store(time.Now().UnixNano())
	})

	return table.getCurrentTables()
}
