package status

import (
	"sync/atomic"
)

var (
	activeSecurityLevel   *uint32
	selectedSecurityLevel *uint32
)

func init() {
	var (
		activeSecurityLevelValue   uint32
		selectedSecurityLevelValue uint32
	)

	activeSecurityLevel = &activeSecurityLevelValue
	selectedSecurityLevel = &selectedSecurityLevelValue
}

// ActiveSecurityLevel returns the current security level.
func ActiveSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(activeSecurityLevel))
}

// SelectedSecurityLevel returns the selected security level.
func SelectedSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(selectedSecurityLevel))
}
