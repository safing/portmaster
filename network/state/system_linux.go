package state

import (
	"github.com/safing/portmaster/network/proc"
	"github.com/safing/portmaster/network/socket"
)

var (
	getTCP4Table = proc.GetTCP4Table
	getTCP6Table = proc.GetTCP6Table
	getUDP4Table = proc.GetUDP4Table
	getUDP6Table = proc.GetUDP6Table
)

func checkConnectionPID(socketInfo *socket.ConnectionInfo, connInbound bool) (pid int, inbound bool, err error) {
	if socketInfo.PID == proc.UnfetchedProcessID {
		socketInfo.PID = proc.FindPID(socketInfo.UID, socketInfo.Inode)
	}
	return socketInfo.PID, connInbound, nil
}

func checkBindPID(socketInfo *socket.BindInfo, connInbound bool) (pid int, inbound bool, err error) {
	if socketInfo.PID == proc.UnfetchedProcessID {
		socketInfo.PID = proc.FindPID(socketInfo.UID, socketInfo.Inode)
	}
	return socketInfo.PID, connInbound, nil
}
