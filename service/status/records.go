package status

import (
	"sync"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster/service/netenv"
)

// SystemStatusRecord describes the overall status of the Portmaster.
// It's a read-only record exposed via runtime:system/status.
type SystemStatusRecord struct {
	record.Base
	sync.Mutex

	// OnlineStatus holds the current online status as
	// seen by the netenv package.
	OnlineStatus netenv.OnlineStatus
	// CaptivePortal holds all information about the captive
	// portal of the network the portmaster is currently
	// connected to, if any.
	CaptivePortal *netenv.CaptivePortal
}
