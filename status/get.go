package status

import (
	"sync/atomic"

	"github.com/Safing/portbase/config"
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

// GetCurrentSecurityLevel returns the current security level.
func GetCurrentSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(currentSecurityLevel))
}

// GetSelectedSecurityLevel returns the selected security level.
func GetSelectedSecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(selectedSecurityLevel))
}

// GetThreatLevel returns the current threat level.
func GetThreatLevel() uint8 {
	return uint8(atomic.LoadUint32(threatLevel))
}

// GetPortmasterStatus returns the current Portmaster status.
func GetPortmasterStatus() uint8 {
	return uint8(atomic.LoadUint32(portmasterStatus))
}

// GetGate17Status returns the current Gate17 status.
func GetGate17Status() uint8 {
	return uint8(atomic.LoadUint32(gate17Status))
}

// GetConfigByLevel returns whether the given security level dependent config option is on or off.
func GetConfigByLevel(name string) func() bool {
	activatedLevel := config.GetAsInt(name, int64(SecurityLevelDynamic))
	return func() bool {
		return uint8(activatedLevel()) <= GetCurrentSecurityLevel()
	}
}
