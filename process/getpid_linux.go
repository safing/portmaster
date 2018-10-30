package process

import "github.com/Safing/portmaster/process/proc"

var (
	getTCP4PacketInfo      = proc.GetTCP4PacketInfo
	getTCP6PacketInfo      = proc.GetTCP6PacketInfo
	getUDP4PacketInfo      = proc.GetUDP4PacketInfo
	getUDP6PacketInfo      = proc.GetUDP6PacketInfo
	getActiveConnectionIDs = proc.GetActiveConnectionIDs
)
