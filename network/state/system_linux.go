package state

import (
	"time"

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
	for i := 0; i <= lookupRetries; i++ {
		// look for PID
		pid = proc.FindConnectionPID(socketInfo)
		if pid != socket.UnidentifiedProcessID {
			// if we found a PID, return
			break
		}

		// every time, except for the last iteration
		if i < lookupRetries {
			// we found no PID, we could have been too fast, give the kernel some time to think
			// back off timer: with 3ms baseWaitTime: 3, 6, 9, 12, 15, 18, 21ms - 84ms in total
			time.Sleep(time.Duration(i+1) * baseWaitTime)
		}
	}

	return pid, connInbound, nil
}

func checkBindPID(socketInfo *socket.BindInfo, connInbound bool) (pid int, inbound bool, err error) {
	for i := 0; i <= lookupRetries; i++ {
		// look for PID
		pid = proc.FindBindPID(socketInfo)
		if pid != socket.UnidentifiedProcessID {
			// if we found a PID, return
			break
		}

		// every time, except for the last iteration
		if i < lookupRetries {
			// we found no PID, we could have been too fast, give the kernel some time to think
			// back off timer: with 3ms baseWaitTime: 3, 6, 9, 12, 15, 18, 21ms - 84ms in total
			time.Sleep(time.Duration(i+1) * baseWaitTime)
		}
	}

	return pid, connInbound, nil
}
