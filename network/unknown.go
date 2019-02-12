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

// GetUnknownConnection returns the connection to a packet of unknown owner.
func GetUnknownConnection(pkt packet.Packet) (*Connection, error) {
	if pkt.IsInbound() {
		switch netutils.ClassifyIP(pkt.GetIPHeader().Src) {
		case netutils.HostLocal:
			return getOrCreateUnknownConnection(pkt, IncomingHost)
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			return getOrCreateUnknownConnection(pkt, IncomingLAN)
		case netutils.Global, netutils.GlobalMulticast:
			return getOrCreateUnknownConnection(pkt, IncomingInternet)
		case netutils.Invalid:
			return getOrCreateUnknownConnection(pkt, IncomingInvalid)
		}
	}

	switch netutils.ClassifyIP(pkt.GetIPHeader().Dst) {
	case netutils.HostLocal:
		return getOrCreateUnknownConnection(pkt, PeerHost)
	case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
		return getOrCreateUnknownConnection(pkt, PeerLAN)
	case netutils.Global, netutils.GlobalMulticast:
		return getOrCreateUnknownConnection(pkt, PeerInternet)
	case netutils.Invalid:
		return getOrCreateUnknownConnection(pkt, PeerInvalid)
	}

	// this should never happen
	return getOrCreateUnknownConnection(pkt, PeerInvalid)
}

func getOrCreateUnknownConnection(pkt packet.Packet, connClass string) (*Connection, error) {
	connection, ok := GetConnection(process.UnknownProcess.Pid, connClass)
	if !ok {
		connection = &Connection{
			Domain:               connClass,
			Direction:            pkt.IsInbound(),
			Verdict:              DROP,
			Reason:               ReasonUnknownProcess,
			process:              process.UnknownProcess,
			Inspect:              true,
			FirstLinkEstablished: time.Now().Unix(),
		}
		if pkt.IsOutbound() {
			connection.Verdict = BLOCK
		}
	}
	connection.process.AddConnection()
	return connection, nil
}
