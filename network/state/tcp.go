package state

import (
	"sync"

	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/network/socket"
)

type tcpTable struct {
	version int

	connections []*socket.ConnectionInfo
	listeners   []*socket.BindInfo
	lock        sync.RWMutex

	fetchOnceAgain utils.OnceAgain
	fetchTable     func() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error)
}

var (
	tcp4Table = &tcpTable{
		version:    4,
		fetchTable: getTCP4Table,
	}

	tcp6Table = &tcpTable{
		version:    6,
		fetchTable: getTCP6Table,
	}
)
