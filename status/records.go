package status

import (
	"sync"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster/netenv"
)

// SystemStatusRecord describes the overall status of the Portmaster.
// It's a read-only record exposed via runtime:system/status.
type SystemStatusRecord struct {
	record.Base
	sync.Mutex

	// ActiveSecurityLevel holds the currently
	// active security level.
	ActiveSecurityLevel uint8
	// SelectedSecurityLevel holds the security level
	// as selected by the user.
	SelectedSecurityLevel uint8
	// ThreatMitigationLevel holds the security level
	// as selected by the auto-pilot.
	ThreatMitigationLevel uint8
	// OnlineStatus holds the current online status as
	// seen by the netenv package.
	OnlineStatus netenv.OnlineStatus
	// CaptivePortal holds all information about the captive
	// portal of the network the portmaster is currently
	// connected to, if any.
	CaptivePortal *netenv.CaptivePortal
}

// SelectedSecurityLevelRecord is used as a dummy record.Record
// to provide a simply runtime-configuration for the user.
// It is write-only and exposed at "runtime:system/security-level".
type SelectedSecurityLevelRecord struct {
	record.Base
	sync.Mutex

	SelectedSecurityLevel uint8
}
