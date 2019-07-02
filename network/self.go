package network

import (
	"fmt"
	"os"
	"time"

	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
)

// GetOwnComm returns the communication for the given packet, that originates from
func GetOwnComm(pkt packet.Packet) (*Communication, error) {
	var domain string

	// Incoming
	if pkt.IsInbound() {
		switch netutils.ClassifyIP(pkt.Info().RemoteIP()) {
		case netutils.HostLocal:
			domain = IncomingHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			domain = IncomingLAN
		case netutils.Global, netutils.GlobalMulticast:
			domain = IncomingInternet
		case netutils.Invalid:
			domain = IncomingInvalid
		}

		communication, ok := GetCommunication(os.Getpid(), domain)
		if !ok {
			proc, err := process.GetOrFindProcess(pkt.Ctx(), os.Getpid())
			if err != nil {
				return nil, fmt.Errorf("could not get own process")
			}
			communication = &Communication{
				Domain:               domain,
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
		domain = PeerHost
	case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
		domain = PeerLAN
	case netutils.Global, netutils.GlobalMulticast:
		domain = PeerInternet
	case netutils.Invalid:
		domain = PeerInvalid
	}

	communication, ok := GetCommunication(os.Getpid(), domain)
	if !ok {
		proc, err := process.GetOrFindProcess(pkt.Ctx(), os.Getpid())
		if err != nil {
			return nil, fmt.Errorf("could not get own process")
		}
		communication = &Communication{
			Domain:               domain,
			Direction:            Outbound,
			process:              proc,
			Inspect:              true,
			FirstLinkEstablished: time.Now().Unix(),
		}
	}
	communication.process.AddCommunication()
	return communication, nil
}
