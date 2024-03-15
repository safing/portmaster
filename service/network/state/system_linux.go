package state

import (
	"time"

	"github.com/safing/portmaster/service/network/proc"
	"github.com/safing/portmaster/service/network/socket"
)

var (
	getTCP4Table = proc.GetTCP4Table
	getTCP6Table = proc.GetTCP6Table
	getUDP4Table = proc.GetUDP4Table
	getUDP6Table = proc.GetUDP6Table

	checkPIDTries        = 5
	checkPIDBaseWaitTime = 5 * time.Millisecond
)

// CheckPID checks the if socket info already has a PID and if not, tries to find it.
// Depending on the OS, this might be a no-op.
func CheckPID(socketInfo socket.Info, connInbound bool) (pid int, inbound bool, err error) {
	for i := 1; i <= checkPIDTries; i++ {
		// look for PID
		pid = proc.GetPID(socketInfo)
		if pid != socket.UndefinedProcessID {
			// if we found a PID, return
			break
		}

		// every time, except for the last iteration
		if i < checkPIDTries {
			// we found no PID, we could have been too fast, give the kernel some time to think
			// back off timer: with 5ms baseWaitTime: 5, 10, 15, 20, 25 - 75ms in total
			time.Sleep(time.Duration(i) * checkPIDBaseWaitTime)
		}
	}

	return pid, connInbound, nil
}
