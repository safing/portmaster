//+build !windows,!linux

package interception

import (
	"github.com/safing/portbase/log"
)

// start starts the interception.
func start() error {
	log.Info("interception: this platform has no support for packet interception - a lot of functionality will be broken")
	return nil
}

// stop starts the interception.
func stop() error {
	return nil
}
