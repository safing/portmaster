package state

import (
	"github.com/safing/portmaster/service/network/iphelper"
	"github.com/safing/portmaster/service/network/socket"
)

var (
	getTCP4Table = iphelper.GetTCP4Table
	getTCP6Table = iphelper.GetTCP6Table
	getUDP4Table = iphelper.GetUDP4Table
	getUDP6Table = iphelper.GetUDP6Table
)

// CheckPID checks the if socket info already has a PID and if not, tries to find it.
// Depending on the OS, this might be a no-op.
func CheckPID(socketInfo socket.Info, connInbound bool) (pid int, inbound bool, err error) {
	return socketInfo.GetPID(), connInbound, nil
}
