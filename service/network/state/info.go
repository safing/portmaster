package state

import (
	"sync"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/socket"
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

	info.TCP4Connections, info.TCP4Listeners = tcp4Table.updateTables()
	info.UDP4Binds = udp4Table.updateTables()

	if netenv.IPv6Enabled() {
		info.TCP6Connections, info.TCP6Listeners = tcp6Table.updateTables()
		info.UDP6Binds = udp6Table.updateTables()
	}

	info.UpdateMeta()
	return info
}
