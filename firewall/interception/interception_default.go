//+build !windows,!linux

package interception

import (
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// start starts the interception.
func start(_ chan packet.Packet) error {
	log.Critical("interception: this platform has no support for packet interception - a lot of functionality will be broken")
	return nil
}

// stop starts the interception.
func stop() error {
	return nil
}
