package firewall

import (
	"context"
	"errors"

	"github.com/safing/portmaster/service/network"
)

// errTunnelingRemoved is returned whenever a connection asks for SPN tunneling.
// portmasterpro ships without the Safing Private Network.
var errTunnelingRemoved = errors.New("SPN tunneling has been removed from portmasterpro")

// checkTunneling is a no-op: no connection is ever routed through the SPN.
// Split tunneling is handled separately by checkSplitTunneling.
func checkTunneling(_ context.Context, _ *network.Connection) {}

// requestTunneling always fails. It can only be reached if a connection was
// externally set to VerdictRerouteToTunnel, which never happens without SPN.
func requestTunneling(_ context.Context, conn *network.Connection) error {
	conn.Tunneled = false
	return errTunnelingRemoved
}
