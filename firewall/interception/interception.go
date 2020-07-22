package interception

import (
	"flag"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

var (
	// Packets channel for feeding the firewall.
	Packets = make(chan packet.Packet, 1000)

	disableInterception bool
)

func init() {
	flag.BoolVar(&disableInterception, "disable-interception", false, "disable packet interception - this breaks a lot of functionality")
}

// Start starts the interception.
func Start() error {
	if disableInterception {
		log.Warning("interception: packet interception is disabled via flag - this breaks a lot of functionality")
		return nil
	}

	return start()
}

// Stop starts the interception.
func Stop() error {
	if disableInterception {
		return nil
	}

	return stop()
}
