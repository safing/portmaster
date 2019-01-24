package status

import (
	"sync/atomic"
)

var (
	currentSecurityLevel  *uint32
	selectedSecurityLevel *uint32
	threatLevel           *uint32
	portmasterStatus      *uint32
	gate17Status          *uint32
)

func init() {
	var (
		currentSecurityLevelValue  uint32
		selectedSecurityLevelValue uint32
		threatLevelValue           uint32
		portmasterStatusValue      uint32
		gate17StatusValue          uint32
	)

	currentSecurityLevel = &currentSecurityLevelValue
	selectedSecurityLevel = &selectedSecurityLevelValue
	threatLevel = &threatLevelValue
	portmasterStatus = &portmasterStatusValue
	gate17Status = &gate17StatusValue
}

// CurrentSecurityLevel returns the current security level.
func CurrentSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(currentSecurityLevel))
}

// SelectedSecurityLevel returns the selected security level.
func SelectedSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(selectedSecurityLevel))
}

// ThreatLevel returns the current threat level.
func ThreatLevel() uint8 {
	return uint8(atomic.LoadUint32(threatLevel))
}

// PortmasterStatus returns the current Portmaster status.
func PortmasterStatus() uint8 {
	return uint8(atomic.LoadUint32(portmasterStatus))
}

// Gate17Status returns the current Gate17 status.
func Gate17Status() uint8 {
	return uint8(atomic.LoadUint32(gate17Status))
}
