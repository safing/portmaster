package interception

import (
	"fmt"

	"github.com/Safing/portmaster/firewall/interception/windivert"
	"github.com/Safing/portmaster/network/packet"
)

var Packets chan packet.Packet

func init() {
	// Packets channel for feeding the firewall.
	Packets = make(chan packet.Packet, 1000)
}

// Start starts the interception.
func Start() error {

	wd, err := windivert.New("/WinDivert.dll", "")
	if err != nil {
		return fmt.Errorf("firewall/interception: could not init windivert: %s", err)
	}

	return wd.Packets(Packets)
}

// Stop starts the interception.
func Stop() error {
	return nil
}
