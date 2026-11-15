package firewall

import (
	"context"
	"errors"

	"github.com/safing/portmaster/service/network"
)

var errTunnelingDisabled = errors.New("SPN tunneling is disabled")

func checkTunneling(ctx context.Context, conn *network.Connection) {
	// SPN tunneling removed; traffic goes through Split Tunneling (upstream proxy / xray).
}

func requestTunneling(ctx context.Context, conn *network.Connection) error {
	return errTunnelingDisabled
}
