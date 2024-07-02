//go:build !windows && !linux

package interception

import (
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// start starts the interception.
func startInterception(_ chan packet.Packet) error {
	log.Critical("interception: this platform has no support for packet interception - a lot of functionality will be broken")
	return nil
}

// stop starts the interception.
func stopInterception() error {
	return nil
}

// ResetVerdictOfAllConnections resets all connections so they are forced to go thought the firewall again.
func ResetVerdictOfAllConnections() error {
	return nil
}

// UpdateVerdictOfConnection updates the verdict of the given connection in the OS integration.
func UpdateVerdictOfConnection(conn *network.Connection) error {
	return nil
}
