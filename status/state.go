package status

import (
	"sync/atomic"
)

var (
	activeLevel   = new(uint32)
	selectedLevel = new(uint32)
)

func setActiveLevel(lvl uint8) {
	atomic.StoreUint32(activeLevel, uint32(lvl))
}

func setSelectedLevel(lvl uint8) {
	atomic.StoreUint32(selectedLevel, uint32(lvl))
}

// ActiveSecurityLevel returns the currently active security
// level.
func ActiveSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(activeLevel))
}

// SelectedSecurityLevel returns the security level as selected
// by the user.
func SelectedSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(selectedLevel))
}
