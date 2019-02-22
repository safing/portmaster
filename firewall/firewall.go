package firewall

import (
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
	// Yes, this would normally be 127.0.0.0/8
	// TODO: figure out any side effects

	localhost4 = net.IPv4(127, 0, 0, 1)
	localhost6 = net.IPv6loopback

	tunnelNet4   *net.IPNet
	tunnelNet6   *net.IPNet
	tunnelEntry4 = net.IPv4(127, 0, 0, 17)
	tunnelEntry6 = net.ParseIP("fd17::17")
)

func init() {
	modules.Register("firewall", prep, start, stop, "core", "network", "nameserver", "profile")
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

	return interception.Start()
}

func stop() error {
	return interception.Stop()
}

func handlePacket(pkt packet.Packet) {

	// log.Tracef("handling packet: %s", pkt)

	// allow local dns
	if pkt.MatchesIP(packet.Remote, localNet4) && pkt.GetTCPUDPHeader() != nil && pkt.GetTCPUDPHeader().DstPort == 53 {
		pkt.PermanentAccept()
		return
	}

	// allow ICMP and IGMP
	// TODO: actually handle these
	switch pkt.GetIPHeader().Protocol {
	case packet.ICMP:
		pkt.PermanentAccept()
		return
	case packet.ICMPv6:
		pkt.PermanentAccept()
		return
	case packet.IGMP:
		pkt.PermanentAccept()
		return
	}

	// log.Debugf("firewall: pkt %s has ID %s", pkt, pkt.GetLinkID())

	// use this to time how long it takes process packet
	// timed := time.Now()
	// defer log.Tracef("firewall: took %s to process packet %s", time.Now().Sub(timed).String(), pkt)

	// check if packet is destined for tunnel
	// switch pkt.IPVersion() {
	// case packet.IPv4:
	// 	if TunnelNet4 != nil && TunnelNet4.Contains(pkt.GetIPHeader().Dst) {
	// 		tunnelHandler(pkt)
	// 	}
	// case packet.IPv6:
	// 	if TunnelNet6 != nil && TunnelNet6.Contains(pkt.GetIPHeader().Dst) {
	// 		tunnelHandler(pkt)
	// 	}
	// }

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
	verdict(pkt, link.GetVerdict())

}

func initialHandler(pkt packet.Packet, link *network.Link) {

	// get Communication
	comm, err := network.GetCommunicationByFirstPacket(pkt)
	if err != nil {
		// get "unknown" comm
		link.Deny(fmt.Sprintf("could not get process: %s", err))
		comm, err = network.GetUnknownCommunication(pkt)

		if err != nil {
			// all failed
			log.Errorf("firewall: could not get unknown comm (dropping %s): %s", pkt.String(), err)
			link.UpdateVerdict(network.VerdictDrop)
			verdict(pkt, network.VerdictDrop)
			link.StopFirewallHandler()
			return
		}

	}

	// add new Link to Communication (and save both)
	comm.AddLink(link)

	// reroute dns requests to nameserver
	if comm.Process().Pid != os.Getpid() && pkt.IsOutbound() && pkt.GetTCPUDPHeader() != nil && !pkt.GetIPHeader().Dst.Equal(localhost) && pkt.GetTCPUDPHeader().DstPort == 53 {
		link.RerouteToNameserver()
		verdict(pkt, link.GetVerdict())
		link.StopFirewallHandler()
		return
	}

	// check if communication needs reevaluation
	if comm.NeedsReevaluation() {
		comm.ResetVerdict()
	}

	// make a decision if not made already
	switch comm.GetVerdict() {
	case network.VerdictUndecided, network.VerdictUndeterminable:
		DecideOnCommunication(comm, pkt)
	}

	switch comm.GetVerdict() {
	case network.VerdictUndecided, network.VerdictUndeterminable, network.VerdictAccept:
		DecideOnLink(comm, link, pkt)
	default:
		link.UpdateVerdict(comm.GetVerdict())
	}

	// log decision
	logInitialVerdict(link)

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
		verdict(pkt, link.GetVerdict())
	}

}

func inspectThenVerdict(pkt packet.Packet, link *network.Link) {
	pktVerdict, continueInspection := inspection.RunInspectors(pkt, link)
	if continueInspection {
		// do not allow to circumvent link decision: e.g. to ACCEPT packets from a DROP-ed link
		linkVerdict := link.GetVerdict()
		if pktVerdict > linkVerdict {
			verdict(pkt, pktVerdict)
		} else {
			verdict(pkt, linkVerdict)
		}
		return
	}

	// we are done with inspecting
	link.StopFirewallHandler()

	link.Lock()
	defer link.Unlock()
	link.VerdictPermanent = permanentVerdicts()
	if link.VerdictPermanent {
		go link.Save()
		permanentVerdict(pkt, link.Verdict)
	} else {
		verdict(pkt, link.Verdict)
	}
}

func permanentVerdict(pkt packet.Packet, action network.Verdict) {
	switch action {
	case network.VerdictAccept:
		atomic.AddUint64(packetsAccepted, 1)
		pkt.PermanentAccept()
		return
	case network.VerdictBlock:
		atomic.AddUint64(packetsBlocked, 1)
		pkt.PermanentBlock()
		return
	case network.VerdictDrop:
		atomic.AddUint64(packetsDropped, 1)
		pkt.PermanentDrop()
		return
	case network.VerdictRerouteToNameserver:
		pkt.RerouteToNameserver()
		return
	case network.VerdictRerouteToTunnel:
		pkt.RerouteToTunnel()
		return
	}
	pkt.Drop()
}

func verdict(pkt packet.Packet, action network.Verdict) {
	switch action {
	case network.VerdictAccept:
		atomic.AddUint64(packetsAccepted, 1)
		pkt.Accept()
		return
	case network.VerdictBlock:
		atomic.AddUint64(packetsBlocked, 1)
		pkt.Block()
		return
	case network.VerdictDrop:
		atomic.AddUint64(packetsDropped, 1)
		pkt.Drop()
		return
	case network.VerdictRerouteToNameserver:
		pkt.RerouteToNameserver()
		return
	case network.VerdictRerouteToTunnel:
		pkt.RerouteToTunnel()
		return
	}
	pkt.Drop()
}

// func tunnelHandler(pkt packet.Packet) {
// 	tunnelInfo := GetTunnelInfo(pkt.GetIPHeader().Dst)
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

func logInitialVerdict(link *network.Link) {
	// switch link.GetVerdict() {
	// case network.VerdictAccept:
	// 	log.Infof("firewall: accepting new link: %s", link.String())
	// case network.VerdictBlock:
	// 	log.Infof("firewall: blocking new link: %s", link.String())
	// case network.VerdictDrop:
	// 	log.Infof("firewall: dropping new link: %s", link.String())
	// case network.VerdictRerouteToNameserver:
	// 	log.Infof("firewall: rerouting new link to nameserver: %s", link.String())
	// case network.VerdictRerouteToTunnel:
	// 	log.Infof("firewall: rerouting new link to tunnel: %s", link.String())
	// }
}

func logChangedVerdict(link *network.Link) {
	// switch link.GetVerdict() {
	// case network.VerdictAccept:
	// 	log.Infof("firewall: change! - now accepting link: %s", link.String())
	// case network.VerdictBlock:
	// 	log.Infof("firewall: change! - now blocking link: %s", link.String())
	// case network.VerdictDrop:
	// 	log.Infof("firewall: change! - now dropping link: %s", link.String())
	// }
}

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
