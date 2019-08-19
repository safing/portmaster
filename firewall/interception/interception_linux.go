package interception

import "github.com/safing/portmaster/network/packet"

var (
	// Packets channel for feeding the firewall.
	Packets = make(chan packet.Packet, 1000)
)

// Start starts the interception.
func Start() error {
	return StartNfqueueInterception()
}

// Stop starts the interception.
func Stop() error {
	return StopNfqueueInterception()
}
