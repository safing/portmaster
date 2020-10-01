package interception

import "github.com/safing/portmaster/network/packet"

// start starts the interception.
func start(ch chan packet.Packet) error {
	return StartNfqueueInterception(ch)
}

// stop starts the interception.
func stop() error {
	return StopNfqueueInterception()
}
