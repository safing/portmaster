package status

import (
	"fmt"
	"sync"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
)

var (
	status *SystemStatus
)

func init() {
	status = &SystemStatus{
		Threats: make(map[string]*Threat),
	}
	status.SetKey(statusDBKey)
}

// SystemStatus saves basic information about the current system status.
type SystemStatus struct {
	record.Base
	sync.Mutex

	ActiveSecurityLevel   uint8
	SelectedSecurityLevel uint8

	PortmasterStatus    uint8
	PortmasterStatusMsg string

	Gate17Status    uint8
	Gate17StatusMsg string

	ThreatMitigationLevel uint8
	Threats               map[string]*Threat

	UpdateStatus string
}

// Save saves the SystemStatus to the database
func (s *SystemStatus) Save() {
	err := statusDB.Put(s)
	if err != nil {
		log.Errorf("status: could not save status to database: %s", err)
	}
}

// EnsureSystemStatus ensures that the given record is of type SystemStatus and unwraps it, if needed.
func EnsureSystemStatus(r record.Record) (*SystemStatus, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &SystemStatus{}
		err := record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}
		return new, nil
	}

	// or adjust type
	new, ok := r.(*SystemStatus)
	if !ok {
		return nil, fmt.Errorf("record not of type *SystemStatus, but %T", r)
	}
	return new, nil
}

// FmtActiveSecurityLevel returns the current security level as a string.
func FmtActiveSecurityLevel() string {
	status.Lock()
	mitigationLevel := status.ThreatMitigationLevel
	status.Unlock()
	active := ActiveSecurityLevel()
	s := FmtSecurityLevel(active)
	if mitigationLevel > 0 && active != mitigationLevel {
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
