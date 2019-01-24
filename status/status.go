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

// FmtCurrentSecurityLevel returns the current security level as a string.
func FmtCurrentSecurityLevel() string {
	current := CurrentSecurityLevel()
	selected := SelectedSecurityLevel()
	s := FmtSecurityLevel(current)
	if current != selected {
		s += "*"
	}
	return s
}

// FmtSecurityLevel returns the given security level as a string.
func FmtSecurityLevel(level uint8) string {
	switch level {
	case SecurityLevelOff:
		return "Off"
	case SecurityLevelDynamic:
		return "Dynamic"
	case SecurityLevelSecure:
		return "Secure"
	case SecurityLevelFortress:
		return "Fortress"
	case SecurityLevelsDynamicAndSecure:
		return "Dynamic and Secure"
	case SecurityLevelsDynamicAndFortress:
		return "Dynamic and Fortress"
	case SecurityLevelsSecureAndFortress:
		return "Secure and Fortress"
	case SecurityLevelsAll:
		return "Dynamic, Secure and Fortress"
	default:
		return "INVALID"
	}
}
