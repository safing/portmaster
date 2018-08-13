// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package interception

import (
	"github.com/Safing/safing-core/firewall/interception/windivert"
	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/modules"
	"github.com/Safing/safing-core/network/packet"
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
