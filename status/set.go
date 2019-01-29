package status

import (
	"sync/atomic"

	"github.com/Safing/portbase/log"
)

// autopilot automatically adjusts the security level as needed
func (s *SystemStatus) autopilot() {
	// check if users is overruling
	if s.SelectedSecurityLevel > SecurityLevelOff {
		s.ActiveSecurityLevel = s.SelectedSecurityLevel
		atomicUpdateActiveSecurityLevel(s.SelectedSecurityLevel)
		return
	}

	// update active security level
	switch s.ThreatMitigationLevel {
	case SecurityLevelOff:
		s.ActiveSecurityLevel = SecurityLevelDynamic
		atomicUpdateActiveSecurityLevel(SecurityLevelDynamic)
	case SecurityLevelDynamic, SecurityLevelSecure, SecurityLevelFortress:
		s.ActiveSecurityLevel = s.ThreatMitigationLevel
		atomicUpdateActiveSecurityLevel(s.ThreatMitigationLevel)
	default:
		log.Errorf("status: threat mitigation level is set to invalid value: %d", s.ThreatMitigationLevel)
	}
}

// setSelectedSecurityLevel sets the selected security level.
func setSelectedSecurityLevel(level uint8) {
	switch level {
	case SecurityLevelOff, SecurityLevelDynamic, SecurityLevelSecure, SecurityLevelFortress:
		status.Lock()
		defer status.Unlock()

		status.SelectedSecurityLevel = level
		atomicUpdateSelectedSecurityLevel(level)
		status.autopilot()

		go status.Save()
	default:
		log.Errorf("status: tried to set selected security level to invalid value: %d", level)
	}
}

// SetPortmasterStatus sets the current Portmaster status.
func SetPortmasterStatus(pmStatus uint8, msg string) {
	switch pmStatus {
	case StatusOff, StatusError, StatusWarning, StatusOk:
		status.Lock()
		defer status.Unlock()

		status.PortmasterStatus = pmStatus
		status.PortmasterStatusMsg = msg
		atomicUpdatePortmasterStatus(pmStatus)

		go status.Save()
	default:
		log.Errorf("status: tried to set portmaster to invalid status: %d", status)
	}
}

// SetGate17Status sets the current Gate17 status.
func SetGate17Status(g17Status uint8, msg string) {
	switch g17Status {
	case StatusOff, StatusError, StatusWarning, StatusOk:
		status.Lock()
		defer status.Unlock()

		status.Gate17Status = g17Status
		status.Gate17StatusMsg = msg
		atomicUpdateGate17Status(g17Status)

		go status.Save()
	default:
		log.Errorf("status: tried to set gate17 to invalid status: %d", status)
	}
}

// update functions for atomic stuff
func atomicUpdateActiveSecurityLevel(level uint8) {
	atomic.StoreUint32(activeSecurityLevel, uint32(level))
}

func atomicUpdateSelectedSecurityLevel(level uint8) {
	atomic.StoreUint32(selectedSecurityLevel, uint32(level))
}

func atomicUpdatePortmasterStatus(status uint8) {
	atomic.StoreUint32(portmasterStatus, uint32(status))
}

func atomicUpdateGate17Status(status uint8) {
	atomic.StoreUint32(gate17Status, uint32(status))
}
