package state

import (
	"sync"

	"github.com/safing/portbase/database/record"

	"github.com/safing/portmaster/network/socket"
)

type StateInfo struct {
	record.Base
	sync.Mutex

	TCP4Connections []*socket.ConnectionInfo
	TCP4Listeners   []*socket.BindInfo
	TCP6Connections []*socket.ConnectionInfo
	TCP6Listeners   []*socket.BindInfo
	UDP4Binds       []*socket.BindInfo
	UDP6Binds       []*socket.BindInfo
}

func GetStateInfo() *StateInfo {
	info := &StateInfo{}

	tcp4Lock.Lock()
	updateTCP4Tables()
	info.TCP4Connections = tcp4Connections
	info.TCP4Listeners = tcp4Listeners
	tcp4Lock.Unlock()

	tcp6Lock.Lock()
	updateTCP6Tables()
	info.TCP6Connections = tcp6Connections
	info.TCP6Listeners = tcp6Listeners
	tcp6Lock.Unlock()

	udp4Lock.Lock()
	updateUDP4Table()
	info.UDP4Binds = udp4Binds
	udp4Lock.Unlock()

	udp6Lock.Lock()
	updateUDP6Table()
	info.UDP6Binds = udp6Binds
	udp6Lock.Unlock()

	info.UpdateMeta()
	return info
}
