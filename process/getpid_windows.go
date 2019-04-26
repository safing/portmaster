package process

import (
	"github.com/Safing/portmaster/process/iphelper"
)

var (
	getTCP4PacketInfo      = iphelper.GetTCP4PacketInfo
	getTCP6PacketInfo      = iphelper.GetTCP6PacketInfo
	getUDP4PacketInfo      = iphelper.GetUDP4PacketInfo
	getUDP6PacketInfo      = iphelper.GetUDP6PacketInfo
	getActiveConnectionIDs = iphelper.GetActiveConnectionIDs
)
