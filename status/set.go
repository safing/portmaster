package status

import (
	"sync/atomic"

	"github.com/safing/portbase/log"
)

// autopilot automatically adjusts the security level as needed.
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
		s.ActiveSecurityLevel = SecurityLevelNormal
		atomicUpdateActiveSecurityLevel(SecurityLevelNormal)
	case SecurityLevelNormal, SecurityLevelHigh, SecurityLevelExtreme:
		s.ActiveSecurityLevel = s.ThreatMitigationLevel
		atomicUpdateActiveSecurityLevel(s.ThreatMitigationLevel)
	default:
		log.Errorf("status: threat mitigation level is set to invalid value: %d", s.ThreatMitigationLevel)
	}
}

// setSelectedSecurityLevel sets the selected security level.
func setSelectedSecurityLevel(level uint8) {
	switch level {
	case SecurityLevelOff, SecurityLevelNormal, SecurityLevelHigh, SecurityLevelExtreme:
		status.Lock()

		status.SelectedSecurityLevel = level
		atomicUpdateSelectedSecurityLevel(level)
		status.autopilot()

		status.Unlock()
		status.Save()
	default:
		log.Errorf("status: tried to set selected security level to invalid value: %d", level)
	}
}

func atomicUpdateActiveSecurityLevel(level uint8) {
	atomic.StoreUint32(activeSecurityLevel, uint32(level))
}

func atomicUpdateSelectedSecurityLevel(level uint8) {
	atomic.StoreUint32(selectedSecurityLevel, uint32(level))
}
