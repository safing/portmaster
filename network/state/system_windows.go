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

	// With a max wait of 5ms, this amounts to up to 25ms,
	// excluding potential data fetching time.
	// Measured on Windows: ~150ms
	lookupTries = 5

	fastLookupTries = 2
)

// CheckPID checks the if socket info already has a PID and if not, tries to find it.
// Depending on the OS, this might be a no-op.
func CheckPID(socketInfo socket.Info, connInbound bool) (pid int, inbound bool, err error) {
	return socketInfo.GetPID(), connInbound, nil
}
