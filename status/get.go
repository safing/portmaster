package status

import (
	"sync/atomic"
)

var (
	activeSecurityLevel   = new(uint32)
	selectedSecurityLevel = new(uint32)
)

// ActiveSecurityLevel returns the current security level.
func ActiveSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(activeSecurityLevel))
}

// SelectedSecurityLevel returns the selected security level.
func SelectedSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(selectedSecurityLevel))
}
