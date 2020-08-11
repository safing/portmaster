package state

import (
	"github.com/safing/portmaster/network/iphelper"
	"github.com/safing/portmaster/network/socket"
)

var (
	getTCP4Table = iphelper.GetTCP4Table
	getTCP6Table = iphelper.GetTCP6Table
	getUDP4Table = iphelper.GetUDP4Table
	getUDP6Table = iphelper.GetUDP6Table
)

func checkPID(socketInfo socket.Info, connInbound bool) (pid int, inbound bool, err error) {
	return socketInfo.GetPID(), connInbound, nil
}
