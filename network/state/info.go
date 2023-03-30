package state

import (
	"sync"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster/netenv"
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

	info.TCP4Connections, info.TCP4Listeners, _ = tcp4Table.updateTables(0)
	info.UDP4Binds, _ = udp4Table.updateTables(0)

	if netenv.IPv6Enabled() {
		info.TCP6Connections, info.TCP6Listeners, _ = tcp6Table.updateTables(tcp6Table.updateIter.Load())
		info.UDP6Binds, _ = udp6Table.updateTables(0)
	}

	info.UpdateMeta()
	return info
}
