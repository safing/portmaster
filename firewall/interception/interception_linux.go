// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package interception

import "github.com/Safing/portmaster/network/packet"

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
