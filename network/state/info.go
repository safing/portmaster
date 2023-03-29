package state

import (
	"sync"

	"github.com/safing/portmaster/netenv"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster/network/socket"
)

// Info holds network state information as provided by the system.
type Info struct {
	record.Base
	sync.Mutex

	TCP4Connections []*socket.ConnectionInfo
	TCP4Listeners   []*socket.BindInfo
	TCP6Connections []*socket.ConnectionInfo
	TCP6Listeners   []*socket.BindInfo
	UDP4Binds       []*socket.BindInfo
	UDP6Binds       []*socket.BindInfo
}

// GetInfo returns all system state tables. The returned data must not be modified.
func GetInfo() *Info {
	info := &Info{}

	tcp4Table.updateTables()
	tcp4Table.lock.RLock()
	info.TCP4Connections = tcp4Table.connections
	info.TCP4Listeners = tcp4Table.listeners
	tcp4Table.lock.RUnlock()

	udp4Table.updateTable()
	udp4Table.lock.RLock()
	info.UDP4Binds = udp4Table.binds
	udp4Table.lock.RUnlock()

	if netenv.IPv6Enabled() {
		tcp6Table.updateTables()
		tcp6Table.lock.RLock()
		info.TCP6Connections = tcp6Table.connections
		info.TCP6Listeners = tcp6Table.listeners
		tcp6Table.lock.RUnlock()

		udp6Table.updateTable()
		udp6Table.lock.RLock()
		info.UDP6Binds = udp6Table.binds
		udp6Table.lock.RUnlock()
	}

	info.UpdateMeta()
	return info
}
