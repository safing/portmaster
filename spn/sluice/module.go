package sluice

import (
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/spn/conf"
)

var (
	module *modules.Module

	entrypointInfoMsg = []byte("You have reached the local SPN entry port, but your connection could not be matched to an SPN tunnel.\n")

	// EnableListener indicates if it should start the sluice listeners. Must be set at startup.
	EnableListener bool = true
)

func init() {
	module = modules.Register("sluice", nil, start, stop, "terminal")
}

func start() error {
	// TODO:
	// Listening on all interfaces for now, as we need this for Windows.
	// Handle similarly to the nameserver listener.

	if conf.Client() && EnableListener {
		StartSluice("tcp4", "0.0.0.0:717")
		StartSluice("udp4", "0.0.0.0:717")

		if netenv.IPv6Enabled() {
			StartSluice("tcp6", "[::]:717")
			StartSluice("udp6", "[::]:717")
		} else {
			log.Warningf("spn/sluice: no IPv6 stack detected, disabling IPv6 SPN entry endpoints")
		}
	}

	return nil
}

func stop() error {
	stopAllSluices()
	return nil
}
