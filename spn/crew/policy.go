package crew

import (
	"context"
	"sync"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/terminal"
)

var (
	connectingHubLock sync.Mutex
	connectingHub     *hub.Hub
)

// EnableConnecting enables connecting from this Hub.
func EnableConnecting(my *hub.Hub) {
	connectingHubLock.Lock()
	defer connectingHubLock.Unlock()

	connectingHub = my
}

func checkExitPolicy(request *ConnectRequest) *terminal.Error {
	connectingHubLock.Lock()
	defer connectingHubLock.Unlock()

	// Check if connect requests are allowed.
	if connectingHub == nil {
		return terminal.ErrPermissionDenied.With("connect requests disabled")
	}

	// Create entity.
	entity := (&intel.Entity{
		IP:       request.IP,
		Protocol: uint8(request.Protocol),
		Port:     request.Port,
		Domain:   request.Domain,
	}).Init(0)
	entity.FetchData(context.TODO())

	// Check against policy.
	result, reason := connectingHub.GetInfo().ExitPolicy().Match(context.TODO(), entity)
	if result == endpoints.Denied {
		return terminal.ErrPermissionDenied.With("connect request for %s violates the exit policy: %s", request, reason)
	}

	return nil
}
