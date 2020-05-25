package state

import (
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

		table.binds = binds
	})
}
