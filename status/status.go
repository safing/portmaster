package status

import "sync"

var (
	sysStatus     *SystemStatus
	sysStatusLock sync.RWMutex
)

func init() {
	sysStatus = &SystemStatus{}
}

// SystemStatus saves basic information about the current system status.
type SystemStatus struct {
	// database.Base
	CurrentSecurityLevel  uint8
	SelectedSecurityLevel uint8

	ThreatLevel  uint8  `json:",omitempty" bson:",omitempty"`
	ThreatReason string `json:",omitempty" bson:",omitempty"`

	PortmasterStatus    uint8  `json:",omitempty" bson:",omitempty"`
	PortmasterStatusMsg string `json:",omitempty" bson:",omitempty"`

	Gate17Status    uint8  `json:",omitempty" bson:",omitempty"`
	Gate17StatusMsg string `json:",omitempty" bson:",omitempty"`
}

// FmtSecurityLevel returns the current security level as a string.
func FmtSecurityLevel() string {
	current := CurrentSecurityLevel()
	selected := SelectedSecurityLevel()
	var s string
	switch current {
	case SecurityLevelOff:
		s = "Off"
	case SecurityLevelDynamic:
		s = "Dynamic"
	case SecurityLevelSecure:
		s = "Secure"
	case SecurityLevelFortress:
		s = "Fortress"
	}
	if current != selected {
		s += "*"
	}
	return s
}
