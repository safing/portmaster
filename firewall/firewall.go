package firewall

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"
	"github.com/Safing/portmaster/firewall/inspection"
	"github.com/Safing/portmaster/firewall/interception"
	"github.com/Safing/portmaster/network"
	"github.com/Safing/portmaster/network/packet"

	// module dependencies
	_ "github.com/Safing/portmaster/core"
	_ "github.com/Safing/portmaster/profile"
)

var (
	// localNet        net.IPNet
	localhost       net.IP
	dnsServer       net.IPNet
	packetsAccepted *uint64
	packetsBlocked  *uint64
	packetsDropped  *uint64

	localNet4 *net.IPNet

	localhost4 = net.IPv4(127, 0, 0, 1)
	localhost6 = net.IPv6loopback

	tunnelNet4   *net.IPNet
	tunnelNet6   *net.IPNet
	tunnelEntry4 = net.IPv4(127, 0, 0, 17)
	tunnelEntry6 = net.ParseIP("fd17::17")
)

func init() {
	modules.Register("firewall", prep, start, stop, "core", "network", "nameserver", "profile", "updates")
}

func prep() (err error) {

	err = registerConfig()
	if err != nil {
		return err
	}

	_, localNet4, err = net.ParseCIDR("127.0.0.0/24")
	// Yes, this would normally be 127.0.0.0/8
	// TODO: figure out any side effects
	if err != nil {
		return fmt.Errorf("firewall: failed to parse cidr 127.0.0.0/24: %s", err)
	}

	_, tunnelNet4, err = net.ParseCIDR("127.17.0.0/16")
	if err != nil {
		return fmt.Errorf("firewall: failed to parse cidr 127.17.0.0/16: %s", err)
	}
	_, tunnelNet6, err = net.ParseCIDR("fd17::/64")
	if err != nil {
		return fmt.Errorf("firewall: failed to parse cidr fd17::/64: %s", err)
	}

	var pA uint64
	packetsAccepted = &pA
	var pB uint64
	packetsBlocked = &pB
	var pD uint64
	packetsDropped = &pD

	return nil
}

func start() error {
	go statLogger()
	go run()
	// go run()
	// go run()
	// go run()

	go portsInUseCleaner()

	return interception.Start()
}

func stop() error {
	return interception.Stop()
}

func handlePacket(pkt packet.Packet) {

	// allow localhost, for now
	if pkt.Info().Src.Equal(pkt.Info().Dst) {
		log.Debugf("accepting localhost communication: %s", pkt)
		pkt.PermanentAccept()
		return
	}

	// allow local dns
	if (pkt.Info().DstPort == 53 || pkt.Info().SrcPort == 53) && pkt.Info().Src.Equal(pkt.Info().Dst) {
		log.Debugf("accepting local dns: %s", pkt)
		pkt.PermanentAccept()
		return
	}

	// // redirect dns (if we know that it's not our own request)
	// if pkt.IsOutbound() && intel.RemoteIsActiveNameserver(pkt) {
	// 	log.Debugf("redirecting dns: %s", pkt)
	// 	pkt.RedirToNameserver()
	// }

	// allow ICMP, IGMP and DHCP
	// TODO: actually handle these
	switch pkt.Info().Protocol {
	case packet.ICMP:
		log.Debugf("accepting ICMP: %s", pkt)
		pkt.PermanentAccept()
		return
	case packet.ICMPv6:
		log.Debugf("accepting ICMPv6: %s", pkt)
		pkt.PermanentAccept()
		return
	case packet.IGMP:
		log.Debugf("accepting IGMP: %s", pkt)
		pkt.PermanentAccept()
		return
	case packet.UDP:
		if pkt.Info().DstPort == 67 || pkt.Info().DstPort == 68 {
			log.Debugf("accepting DHCP: %s", pkt)
			pkt.PermanentAccept()
			return
		}
	}

	// log.Debugf("firewall: pkt %s has ID %s", pkt, pkt.GetLinkID())

	// use this to time how long it takes process packet
	// timed := time.Now()
	// defer log.Tracef("firewall: took %s to process packet %s", time.Now().Sub(timed).String(), pkt)

	// check if packet is destined for tunnel
	// switch pkt.IPVersion() {
	// case packet.IPv4:
	// 	if TunnelNet4 != nil && TunnelNet4.Contains(pkt.Info().Dst) {
	// 		tunnelHandler(pkt)
	// 	}
	// case packet.IPv6:
	// 	if TunnelNet6 != nil && TunnelNet6.Contains(pkt.Info().Dst) {
	// 		tunnelHandler(pkt)
	// 	}
	// }

	pkt.SetCtx(log.AddTracer(context.Background()))
	log.Tracer(pkt.Ctx()).Tracef("firewall: handling packet: %s", pkt)

	// associate packet to link and handle
	link, created := network.GetOrCreateLinkByPacket(pkt)
	if created {
		link.SetFirewallHandler(initialHandler)
		link.HandlePacket(pkt)
		return
	}
	if link.FirewallHandlerIsSet() {
		link.HandlePacket(pkt)
		return
	}
	issueVerdict(pkt, link, 0, true, false)
}

func initialHandler(pkt packet.Packet, link *network.Link) {
	log.Tracer(pkt.Ctx()).Trace("firewall: [initial handler]")

	// check for internal firewall bypass
	ps := getPortStatusAndMarkUsed(pkt.Info().LocalPort())
	if ps.isMe {
		// connect to comms
		comm, err := network.GetOwnComm(pkt)
		if err != nil {
			// log.Warningf("firewall: could not get own comm: %s", err)
			log.Tracer(pkt.Ctx()).Warningf("firewall: could not get own comm: %s", err)
		} else {
			comm.AddLink(link)
		}

		// approve
		link.Accept("internally approved")
		log.Tracer(pkt.Ctx()).Tracef("firewall: internally approved link (via local port %d)", pkt.Info().LocalPort())

		// finish
		link.StopFirewallHandler()
		issueVerdict(pkt, link, 0, true, true)

		return
	}

	// get Communication
	comm, err := network.GetCommunicationByFirstPacket(pkt)
	if err != nil {
		log.Tracer(pkt.Ctx()).Warningf("firewall: could not get process, denying link: %s", err)

		// get "unknown" comm
		link.Deny(fmt.Sprintf("could not get process: %s", err))
		comm, err = network.GetUnknownCommunication(pkt)

		if err != nil {
			// all failed
			log.Tracer(pkt.Ctx()).Errorf("firewall: could not get unknown comm: %s", err)
			link.UpdateVerdict(network.VerdictDrop)
			link.StopFirewallHandler()
			issueVerdict(pkt, link, 0, true, true)
			return
		}

	}

	// add new Link to Communication (and save both)
	comm.AddLink(link)
	log.Tracer(pkt.Ctx()).Tracef("firewall: link attached to %s", comm)

	// reroute dns requests to nameserver
	if comm.Process().Pid != os.Getpid() && pkt.IsOutbound() && pkt.Info().DstPort == 53 && !pkt.Info().Src.Equal(pkt.Info().Dst) {
		link.UpdateVerdict(network.VerdictRerouteToNameserver)
		link.StopFirewallHandler()
		issueVerdict(pkt, link, 0, true, true)
		return
	}

	log.Tracer(pkt.Ctx()).Trace("firewall: starting decision process")
	DecideOnCommunication(comm, pkt)
	DecideOnLink(comm, link, pkt)

	// TODO: link this to real status
	// gate17Active := mode.Client()

	switch {
	// case gate17Active && link.Inspect:
	// 	// tunnel link, but also inspect (after reroute)
	// 	link.Tunneled = true
	// 	link.SetFirewallHandler(inspectThenVerdict)
	// 	verdict(pkt, link.GetVerdict())
	// case gate17Active:
	// 	// tunnel link, don't inspect
	// 	link.Tunneled = true
	// 	link.StopFirewallHandler()
	// 	permanentVerdict(pkt, network.VerdictAccept)
	case link.Inspect:
		link.SetFirewallHandler(inspectThenVerdict)
		inspectThenVerdict(pkt, link)
	default:
		link.StopFirewallHandler()
		issueVerdict(pkt, link, 0, true, false)
	}

}

func inspectThenVerdict(pkt packet.Packet, link *network.Link) {
	pktVerdict, continueInspection := inspection.RunInspectors(pkt, link)
	if continueInspection {
		issueVerdict(pkt, link, pktVerdict, false, false)
		return
	}

	// we are done with inspecting
	link.StopFirewallHandler()
	issueVerdict(pkt, link, 0, true, false)
}

func issueVerdict(pkt packet.Packet, link *network.Link, verdict network.Verdict, allowPermanent, forceSave bool) {
	link.Lock()

	// enable permanent verdict
	if allowPermanent && !link.VerdictPermanent {
		link.VerdictPermanent = permanentVerdicts()
		if link.VerdictPermanent {
			forceSave = true
		}
	}

	// do not allow to circumvent link decision: e.g. to ACCEPT packets from a DROP-ed link
	if verdict < link.Verdict {
		verdict = link.Verdict
	}

	switch verdict {
	case network.VerdictAccept:
		atomic.AddUint64(packetsAccepted, 1)
		if link.VerdictPermanent {
			pkt.PermanentAccept()
		} else {
			pkt.Accept()
		}
	case network.VerdictBlock:
		atomic.AddUint64(packetsBlocked, 1)
		if link.VerdictPermanent {
			pkt.PermanentBlock()
		} else {
			pkt.Block()
		}
	case network.VerdictDrop:
		atomic.AddUint64(packetsDropped, 1)
		if link.VerdictPermanent {
			pkt.PermanentDrop()
		} else {
			pkt.Drop()
		}
	case network.VerdictRerouteToNameserver:
		pkt.RerouteToNameserver()
	case network.VerdictRerouteToTunnel:
		pkt.RerouteToTunnel()
	default:
		atomic.AddUint64(packetsDropped, 1)
		pkt.Drop()
	}

	link.Unlock()

	log.InfoTracef(pkt.Ctx(), "firewall: %s %s", link.Verdict, link)

	if forceSave && !link.KeyIsSet() {
		// always save if not yet saved
		go link.Save()
	}
}

// func tunnelHandler(pkt packet.Packet) {
// 	tunnelInfo := GetTunnelInfo(pkt.Info().Dst)
// 	if tunnelInfo == nil {
// 		pkt.Block()
// 		return
// 	}
//
// 	entry.CreateTunnel(pkt, tunnelInfo.Domain, tunnelInfo.RRCache.ExportAllARecords())
// 	log.Tracef("firewall: rerouting %s to tunnel entry point", pkt)
// 	pkt.RerouteToTunnel()
// 	return
// }

func run() {
	for {
		select {
		case <-modules.ShuttingDown():
			return
		case pkt := <-interception.Packets:
			handlePacket(pkt)
		}
	}
}

func statLogger() {
	for {
		select {
		case <-modules.ShuttingDown():
			return
		case <-time.After(10 * time.Second):
			log.Tracef("firewall: packets accepted %d, blocked %d, dropped %d", atomic.LoadUint64(packetsAccepted), atomic.LoadUint64(packetsBlocked), atomic.LoadUint64(packetsDropped))
			atomic.StoreUint64(packetsAccepted, 0)
			atomic.StoreUint64(packetsBlocked, 0)
			atomic.StoreUint64(packetsDropped, 0)
		}
	}
}
