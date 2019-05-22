package network

import (
	"time"

	"github.com/Safing/portmaster/network/netutils"
	"github.com/Safing/portmaster/network/packet"
	"github.com/Safing/portmaster/process"
)

// Static reasons
const (
	ReasonUnknownProcess = "unknown connection owner: process could not be found"
)

// GetUnknownCommunication returns the connection to a packet of unknown owner.
func GetUnknownCommunication(pkt packet.Packet) (*Communication, error) {
	if pkt.IsInbound() {
		switch netutils.ClassifyIP(pkt.Info().Src) {
		case netutils.HostLocal:
			return getOrCreateUnknownCommunication(pkt, IncomingHost)
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			return getOrCreateUnknownCommunication(pkt, IncomingLAN)
		case netutils.Global, netutils.GlobalMulticast:
			return getOrCreateUnknownCommunication(pkt, IncomingInternet)
		case netutils.Invalid:
			return getOrCreateUnknownCommunication(pkt, IncomingInvalid)
		}
	}

	switch netutils.ClassifyIP(pkt.Info().Dst) {
	case netutils.HostLocal:
		return getOrCreateUnknownCommunication(pkt, PeerHost)
	case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
		return getOrCreateUnknownCommunication(pkt, PeerLAN)
	case netutils.Global, netutils.GlobalMulticast:
		return getOrCreateUnknownCommunication(pkt, PeerInternet)
	case netutils.Invalid:
		return getOrCreateUnknownCommunication(pkt, PeerInvalid)
	}

	// this should never happen
	return getOrCreateUnknownCommunication(pkt, PeerInvalid)
}

func getOrCreateUnknownCommunication(pkt packet.Packet, connClass string) (*Communication, error) {
	connection, ok := GetCommunication(process.UnknownProcess.Pid, connClass)
	if !ok {
		connection = &Communication{
			Domain:               connClass,
			Direction:            pkt.IsInbound(),
			Verdict:              VerdictDrop,
			Reason:               ReasonUnknownProcess,
			process:              process.UnknownProcess,
			Inspect:              false,
			FirstLinkEstablished: time.Now().Unix(),
		}
		if pkt.IsOutbound() {
			connection.Verdict = VerdictBlock
		}
	}
	connection.process.AddCommunication()
	return connection, nil
}
