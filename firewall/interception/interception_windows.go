package interception

import (
	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"
	"github.com/Safing/portmaster/firewall/interception/windivert"
	"github.com/Safing/portmaster/network/packet"
)

var Packets chan packet.Packet

func init() {
	Packets = make(chan packet.Packet, 1000)
}

func Start() {

	windivertModule := modules.Register("Firewall:Interception:WinDivert", 192)

	wd, err := windivert.New("/WinDivert.dll", "")
	if err != nil {
		log.Criticalf("firewall/interception: could not init windivert: %s", err)
	} else {
		wd.Packets(Packets)
	}

	<-windivertModule.Stop
	windivertModule.StopComplete()
}
