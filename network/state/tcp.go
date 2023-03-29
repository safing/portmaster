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

	dualStack *tcpTable
}

var (
	tcp6Table = &tcpTable{
		version:    6,
		fetchTable: getTCP6Table,
	}

	tcp4Table = &tcpTable{
		version:    4,
		fetchTable: getTCP4Table,
	}
)

// EnableTCPDualStack adds the TCP6 table to the TCP4 table as a dual-stack.
// Must be called before any lookup operation.
func EnableTCPDualStack() {
	tcp4Table.dualStack = tcp6Table
}
