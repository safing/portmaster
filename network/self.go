package network

import (
	"fmt"
	"os"
	"time"

	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
)

// GetOwnComm returns the communication for the given packet, that originates from the Portmaster itself.
func GetOwnComm(pkt packet.Packet) (*Communication, error) {
	var scope string

	// Incoming
	if pkt.IsInbound() {
		switch netutils.ClassifyIP(pkt.Info().RemoteIP()) {
		case netutils.HostLocal:
			scope = IncomingHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			scope = IncomingLAN
		case netutils.Global, netutils.GlobalMulticast:
			scope = IncomingInternet
		case netutils.Invalid:
			scope = IncomingInvalid
		}

		communication, ok := GetCommunication(os.Getpid(), scope)
		if !ok {
			proc, err := process.GetOrFindProcess(pkt.Ctx(), os.Getpid())
			if err != nil {
				return nil, fmt.Errorf("could not get own process")
			}
			communication = &Communication{
				Scope:                scope,
				Entity:               (&intel.Entity{}).Init(),
				Direction:            Inbound,
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
			}
		}
		communication.process.AddCommunication()
		return communication, nil
	}

	// PeerToPeer
	switch netutils.ClassifyIP(pkt.Info().RemoteIP()) {
	case netutils.HostLocal:
		scope = PeerHost
	case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
		scope = PeerLAN
	case netutils.Global, netutils.GlobalMulticast:
		scope = PeerInternet
	case netutils.Invalid:
		scope = PeerInvalid
	}

	communication, ok := GetCommunication(os.Getpid(), scope)
	if !ok {
		proc, err := process.GetOrFindProcess(pkt.Ctx(), os.Getpid())
		if err != nil {
			return nil, fmt.Errorf("could not get own process")
		}
		communication = &Communication{
			Scope:                scope,
			Entity:               (&intel.Entity{}).Init(),
			Direction:            Outbound,
			process:              proc,
			Inspect:              true,
			FirstLinkEstablished: time.Now().Unix(),
		}
	}
	communication.process.AddCommunication()
	return communication, nil
}
