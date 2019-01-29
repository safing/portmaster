package status

import (
	"sync/atomic"
)

var (
	activeSecurityLevel   *uint32
	selectedSecurityLevel *uint32
	portmasterStatus      *uint32
	gate17Status          *uint32
)

func init() {
	var (
		activeSecurityLevelValue   uint32
		selectedSecurityLevelValue uint32
		portmasterStatusValue      uint32
		gate17StatusValue          uint32
	)

	activeSecurityLevel = &activeSecurityLevelValue
	selectedSecurityLevel = &selectedSecurityLevelValue
	portmasterStatus = &portmasterStatusValue
	gate17Status = &gate17StatusValue
}

// ActiveSecurityLevel returns the current security level.
func ActiveSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(activeSecurityLevel))
}

// SelectedSecurityLevel returns the selected security level.
func SelectedSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(selectedSecurityLevel))
}

// PortmasterStatus returns the current Portmaster status.
func PortmasterStatus() uint8 {
	return uint8(atomic.LoadUint32(portmasterStatus))
}

// Gate17Status returns the current Gate17 status.
func Gate17Status() uint8 {
	return uint8(atomic.LoadUint32(gate17Status))
}
