package status

import (
	"strings"
	"sync"
)

// Threat describes a detected threat.
type Threat struct {
	ID              string      // A unique ID chosen by reporting module (eg. modulePrefix-incident) to periodically check threat existence
	Name            string      // Descriptive (human readable) name for detected threat
	Description     string      // Simple description
	AdditionalData  interface{} // Additional data a module wants to make available for the user
	MitigationLevel uint8       // Recommended Security Level to switch to for mitigation
	Started         int64
	Ended           int64
	// TODO: add locking
}

// AddOrUpdateThreat adds or updates a new threat in the system status.
func AddOrUpdateThreat(new *Threat) {
	status.Lock()
	defer status.Unlock()

	status.Threats[new.ID] = new
	status.updateThreatMitigationLevel()
	status.autopilot()

	go status.Save()
}

// DeleteThreat deletes a threat from the system status.
func DeleteThreat(id string) {
	status.Lock()
	defer status.Unlock()

	delete(status.Threats, id)
	status.updateThreatMitigationLevel()
	status.autopilot()

	go status.Save()
}

// GetThreats returns all threats who's IDs are prefixed by the given string, and also a locker for editing them.
func GetThreats(idPrefix string) ([]*Threat, sync.Locker) {
	status.Lock()
	defer status.Unlock()

	var exportedThreats []*Threat
	for id, threat := range status.Threats {
		if strings.HasPrefix(id, idPrefix) {
			exportedThreats = append(exportedThreats, threat)
		}
	}

	return exportedThreats, &status.Mutex
}

func (s *SystemStatus) updateThreatMitigationLevel() {
	// get highest mitigationLevel
	var mitigationLevel uint8
	for _, threat := range s.Threats {
		switch threat.MitigationLevel {
		case SecurityLevelNormal, SecurityLevelHigh, SecurityLevelExtreme:
			if threat.MitigationLevel > mitigationLevel {
				mitigationLevel = threat.MitigationLevel
			}
		}
	}

	// set new ThreatMitigationLevel
	s.ThreatMitigationLevel = mitigationLevel
}
