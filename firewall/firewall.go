package firewall

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/firewall/interception"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"

	// module dependencies
	_ "github.com/safing/portmaster/core"
	_ "github.com/safing/portmaster/profile"
)

var (
	module *modules.Module

	// localNet        net.IPNet
	// localhost net.IP
	// dnsServer       net.IPNet
	packetsAccepted *uint64
	packetsBlocked  *uint64
	packetsDropped  *uint64

	// localNet4 *net.IPNet

	localhost4 = net.IPv4(127, 0, 0, 1)
	// localhost6 = net.IPv6loopback

	// tunnelNet4 *net.IPNet
	// tunnelNet6 *net.IPNet
	// tunnelEntry4 = net.IPv4(127, 0, 0, 17)
	// tunnelEntry6 = net.ParseIP("fd17::17")
)

func init() {
	module = modules.Register("filter", prep, start, stop, "core", "network", "nameserver", "intel")
	subsystems.Register(
		"filter",
		"Privacy Filter",
		"DNS and Network Filter",
		module,
		"config:filter/",
		&config.Option{
			Name:           "Enable Privacy Filter",
			Key:            CfgOptionEnableFilterKey,
			Description:    "Enable the Privacy Filter Subsystem to filter DNS queries and network requests.",
			OptType:        config.OptTypeBool,
			ExpertiseLevel: config.ExpertiseLevelUser,
			ReleaseLevel:   config.ReleaseLevelBeta,
			DefaultValue:   true,
		},
	)

	network.SetDefaultFirewallHandler(defaultHandler)
}

func prep() (err error) {
	err = registerConfig()
	if err != nil {
		return err
	}

	err = prepAPIAuth()
	if err != nil {
		return err
	}

	// _, localNet4, err = net.ParseCIDR("127.0.0.0/24")
	// // Yes, this would normally be 127.0.0.0/8
	// // TODO: figure out any side effects
	// if err != nil {
	// 	return fmt.Errorf("filter: failed to parse cidr 127.0.0.0/24: %s", err)
	// }

	// _, tunnelNet4, err = net.ParseCIDR("127.17.0.0/16")
	// if err != nil {
	// 	return fmt.Errorf("filter: failed to parse cidr 127.17.0.0/16: %s", err)
	// }
	// _, tunnelNet6, err = net.ParseCIDR("fd17::/64")
	// if err != nil {
	// 	return fmt.Errorf("filter: failed to parse cidr fd17::/64: %s", err)
	// }

	var pA uint64
	packetsAccepted = &pA
	var pB uint64
	packetsBlocked = &pB
	var pD uint64
	packetsDropped = &pD

	return nil
}

func start() error {
	startAPIAuth()

	module.StartWorker("stat logger", func(ctx context.Context) error {
		statLogger()
		return nil
	})

	module.StartWorker("packet handler", func(ctx context.Context) error {
		run()
		return nil
	})

	module.StartWorker("ports state cleaner", func(ctx context.Context) error {
		portsInUseCleaner()
		return nil
	})

	return interception.Start()
}

func stop() error {
	return interception.Stop()
}

func handlePacket(pkt packet.Packet) {

	// allow localhost, for now
	// if pkt.Info().Src.Equal(pkt.Info().Dst) {
	// 	log.Debugf("accepting localhost communication: %s", pkt)
	// 	pkt.PermanentAccept()
	// 	return
	// }

	// allow local dns
	if (pkt.Info().DstPort == 53 || pkt.Info().SrcPort == 53) &&
		(pkt.Info().Src.Equal(pkt.Info().Dst) || // Windows redirects back to same interface
			pkt.Info().Src.Equal(localhost4) || // Linux sometimes does 127.0.0.1->127.0.0.53
			pkt.Info().Dst.Equal(localhost4)) {
		log.Debugf("accepting local dns: %s", pkt)
		_ = pkt.PermanentAccept()
		return
	}

	// allow api access, if address was parsed successfully
	if apiPortSet {
		if (pkt.Info().DstPort == apiPort || pkt.Info().SrcPort == apiPort) && pkt.Info().Src.Equal(pkt.Info().Dst) {
			log.Debugf("accepting api connection: %s", pkt)
			_ = pkt.PermanentAccept()
			return
		}
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
		_ = pkt.PermanentAccept()
		return
	case packet.ICMPv6:
		log.Debugf("accepting ICMPv6: %s", pkt)
		_ = pkt.PermanentAccept()
		return
	case packet.IGMP:
		log.Debugf("accepting IGMP: %s", pkt)
		_ = pkt.PermanentAccept()
		return
	case packet.UDP:
		if pkt.Info().DstPort == 67 || pkt.Info().DstPort == 68 {
			log.Debugf("accepting DHCP: %s", pkt)
			_ = pkt.PermanentAccept()
			return
		}
		// TODO: Howto handle NetBios?
	}

	// log.Debugf("filter: pkt %s has ID %s", pkt, pkt.GetLinkID())

	// use this to time how long it takes process packet
	// timed := time.Now()
	// defer log.Tracef("filter: took %s to process packet %s", time.Now().Sub(timed).String(), pkt)

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

	traceCtx, tracer := log.AddTracer(context.Background())
	if tracer != nil {
		pkt.SetCtx(traceCtx)
		tracer.Tracef("filter: handling packet: %s", pkt)
	}

	// associate packet to link and handle
	conn, ok := network.GetConnection(pkt.GetConnectionID())
	if ok {
		tracer.Tracef("filter: assigned to connection %s", conn.ID)
	} else {
		conn = network.NewConnectionFromFirstPacket(pkt)
		tracer.Tracef("filter: created new connection %s", conn.ID)
		conn.SetFirewallHandler(initialHandler)
	}

	// handle packet
	conn.HandlePacket(pkt)
}

func initialHandler(conn *network.Connection, pkt packet.Packet) {
	log.Tracer(pkt.Ctx()).Trace("filter: [initial handler]")

	// check for internal firewall bypass
	ps := getPortStatusAndMarkUsed(pkt.Info().LocalPort())
	if ps.isMe {
		// approve
		conn.Accept("internally approved")
		// finish
		conn.StopFirewallHandler()
		issueVerdict(conn, pkt, 0, true)
		return
	}

	// reroute dns requests to nameserver
	if conn.Process().Pid != os.Getpid() && pkt.IsOutbound() && pkt.Info().DstPort == 53 && !pkt.Info().Src.Equal(pkt.Info().Dst) {
		conn.Verdict = network.VerdictRerouteToNameserver
		conn.StopFirewallHandler()
		issueVerdict(conn, pkt, 0, true)
		return
	}

	log.Tracer(pkt.Ctx()).Trace("filter: starting decision process")
	DecideOnConnection(conn, pkt)
	conn.Inspecting = false // TODO: enable inspecting again

	switch {
	case conn.Inspecting:
		log.Tracer(pkt.Ctx()).Trace("filter: start inspecting")
		conn.SetFirewallHandler(inspectThenVerdict)
		inspectThenVerdict(conn, pkt)
	default:
		conn.StopFirewallHandler()
		issueVerdict(conn, pkt, 0, true)
	}

}

func defaultHandler(conn *network.Connection, pkt packet.Packet) {
	issueVerdict(conn, pkt, 0, true)
}

func inspectThenVerdict(conn *network.Connection, pkt packet.Packet) {
	pktVerdict, continueInspection := inspection.RunInspectors(conn, pkt)
	if continueInspection {
		issueVerdict(conn, pkt, pktVerdict, false)
		return
	}

	// we are done with inspecting
	conn.StopFirewallHandler()
	issueVerdict(conn, pkt, 0, true)
}

func issueVerdict(conn *network.Connection, pkt packet.Packet, verdict network.Verdict, allowPermanent bool) {
	// enable permanent verdict
	if allowPermanent && !conn.VerdictPermanent {
		conn.VerdictPermanent = permanentVerdicts()
		if conn.VerdictPermanent {
			conn.SaveWhenFinished()
		}
	}

	// do not allow to circumvent decision: e.g. to ACCEPT packets from a DROP-ed connection
	if verdict < conn.Verdict {
		verdict = conn.Verdict
	}

	var err error
	switch verdict {
	case network.VerdictAccept:
		atomic.AddUint64(packetsAccepted, 1)
		if conn.VerdictPermanent {
			err = pkt.PermanentAccept()
		} else {
			err = pkt.Accept()
		}
	case network.VerdictBlock:
		atomic.AddUint64(packetsBlocked, 1)
		if conn.VerdictPermanent {
			err = pkt.PermanentBlock()
		} else {
			err = pkt.Block()
		}
	case network.VerdictDrop:
		atomic.AddUint64(packetsDropped, 1)
		if conn.VerdictPermanent {
			err = pkt.PermanentDrop()
		} else {
			err = pkt.Drop()
		}
	case network.VerdictRerouteToNameserver:
		err = pkt.RerouteToNameserver()
	case network.VerdictRerouteToTunnel:
		err = pkt.RerouteToTunnel()
	default:
		atomic.AddUint64(packetsDropped, 1)
		err = pkt.Drop()
	}

	if err != nil {
		log.Warningf("filter: failed to apply verdict to pkt %s: %s", pkt, err)
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
// 	log.Tracef("filter: rerouting %s to tunnel entry point", pkt)
// 	pkt.RerouteToTunnel()
// 	return
// }

func run() {
	for {
		select {
		case <-module.Stopping():
			return
		case pkt := <-interception.Packets:
			handlePacket(pkt)
		}
	}
}

func statLogger() {
	for {
		select {
		case <-module.Stopping():
			return
		case <-time.After(10 * time.Second):
			log.Tracef("filter: packets accepted %d, blocked %d, dropped %d", atomic.LoadUint64(packetsAccepted), atomic.LoadUint64(packetsBlocked), atomic.LoadUint64(packetsDropped))
			atomic.StoreUint64(packetsAccepted, 0)
			atomic.StoreUint64(packetsBlocked, 0)
			atomic.StoreUint64(packetsDropped, 0)
		}
	}
}
