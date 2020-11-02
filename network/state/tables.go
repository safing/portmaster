package state

import (
	"net"

	"github.com/safing/portbase/log"
)

func (table *tcpTable) updateTables() {
	table.fetchOnceAgain.Do(func() {
		table.lock.Lock()
		defer table.lock.Unlock()

		connections, listeners, err := table.fetchTable()
		if err != nil {
			log.Warningf("state: failed to get TCP%d socket table: %s", table.version, err)
			return
		}

		// Pre-check for any listeners.
		for _, bindInfo := range listeners {
			bindInfo.ListensAny = bindInfo.Local.IP.Equal(net.IPv4zero) || bindInfo.Local.IP.Equal(net.IPv6zero)
		}

		table.connections = connections
		table.listeners = listeners
	})
}

func (table *udpTable) updateTable() {
	table.fetchOnceAgain.Do(func() {
		table.lock.Lock()
		defer table.lock.Unlock()

		binds, err := table.fetchTable()
		if err != nil {
			log.Warningf("state: failed to get UDP%d socket table: %s", table.version, err)
			return
		}

		// Pre-check for any listeners.
		for _, bindInfo := range binds {
			bindInfo.ListensAny = bindInfo.Local.IP.Equal(net.IPv4zero) || bindInfo.Local.IP.Equal(net.IPv6zero)
		}

		table.binds = binds
	})
}
