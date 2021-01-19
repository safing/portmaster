package firewall

import (
	"context"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/netenv"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/firewall/interception"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/spn/captain"
	"github.com/safing/spn/sluice"

	// module dependencies
	_ "github.com/safing/portmaster/core/base"
)

var (
	interceptionModule *modules.Module

	nameserverIPMatcher      func(ip net.IP) bool
	nameserverIPMatcherSet   = abool.New()
	nameserverIPMatcherReady = abool.New()

	packetsAccepted = new(uint64)
	packetsBlocked  = new(uint64)
	packetsDropped  = new(uint64)
	packetsFailed   = new(uint64)

	blockedIPv4 = net.IPv4(0, 0, 0, 17)
	blockedIPv6 = net.ParseIP("::17")

	ownPID = os.Getpid()
)

func init() {
	interceptionModule = modules.Register("interception", interceptionPrep, interceptionStart, interceptionStop, "base", "updates", "network")

	network.SetDefaultFirewallHandler(defaultHandler)
}

func interceptionPrep() (err error) {
	return prepAPIAuth()
}

func interceptionStart() error {
	startAPIAuth()

	interceptionModule.StartWorker("stat logger", statLogger)
	interceptionModule.StartWorker("packet handler", packetHandler)
	interceptionModule.StartWorker("ports state cleaner", portsInUseCleaner)

	return interception.Start()
}

func interceptionStop() error {
	return interception.Stop()
}

// SetNameserverIPMatcher sets a function that is used to match the internal
// nameserver IP(s). Can only bet set once.
func SetNameserverIPMatcher(fn func(ip net.IP) bool) error {
	if !nameserverIPMatcherSet.SetToIf(false, true) {
		return errors.New("nameserver IP matcher already set")
	}

	nameserverIPMatcher = fn
	nameserverIPMatcherReady.Set()
	return nil
}

func handlePacket(ctx context.Context, pkt packet.Packet) {
	if fastTrackedPermit(pkt) {
		return
	}

	// Add context tracer and set context on packet.
	traceCtx, tracer := log.AddTracer(ctx)
	if tracer != nil {
		// The trace is submitted in `network.Connection.packetHandler()`.
		tracer.Tracef("filter: handling packet: %s", pkt)
	}
	pkt.SetCtx(traceCtx)

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

// fastTrackedPermit quickly permits certain network criticial or internal connections.
func fastTrackedPermit(pkt packet.Packet) (handled bool) {
	meta := pkt.Info()

	// Check if connection was already blocked.
	if meta.Dst.Equal(blockedIPv4) || meta.Dst.Equal(blockedIPv6) {
		_ = pkt.PermanentBlock()
		return true
	}

	// Some programs do a network self-check where they connects to the same
	// IP/Port to test network capabilities.
	// Eg. dig: https://gitlab.isc.org/isc-projects/bind9/-/issues/1140
	if meta.SrcPort == meta.DstPort &&
		meta.Src.Equal(meta.Dst) {
		log.Debugf("filter: fast-track network self-check: %s", pkt)
		_ = pkt.PermanentAccept()
		return true
	}

	switch meta.Protocol {
	case packet.ICMP:
		// Always permit ICMP.
		log.Debugf("filter: fast-track accepting ICMP: %s", pkt)
		_ = pkt.PermanentAccept()
		return true

	case packet.ICMPv6:
		// Always permit ICMPv6.
		log.Debugf("filter: fast-track accepting ICMPv6: %s", pkt)
		_ = pkt.PermanentAccept()
		return true

	case packet.UDP, packet.TCP:
		switch meta.DstPort {

		case 67, 68, 546, 547:
			// Always allow DHCP, DHCPv6.

			// DHCP and DHCPv6 must be UDP.
			if meta.Protocol != packet.UDP {
				return false
			}

			// DHCP is only valid in local network scopes.
			switch netutils.ClassifyIP(meta.Dst) {
			case netutils.HostLocal, netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			default:
				return false
			}

			// Log and permit.
			log.Debugf("filter: fast-track accepting DHCP: %s", pkt)
			_ = pkt.PermanentAccept()
			return true

		case apiPort:
			// Always allow direct access to the Portmaster API.

			// Portmaster API is TCP only.
			if meta.Protocol != packet.TCP {
				return false
			}

			// Check if the api port is even set.
			if !apiPortSet {
				return false
			}

			// Must be destined for the API IP.
			if !meta.Dst.Equal(apiIP) {
				return false
			}

			// Only fast-track local requests.
			isMe, err := netenv.IsMyIP(meta.Src)
			switch {
			case err != nil:
				log.Debugf("filter: failed to check if %s is own IP for fast-track: %s", meta.Src, err)
				return false
			case !isMe:
				return false
			}

			// Log and permit.
			log.Debugf("filter: fast-track accepting api connection: %s", pkt)
			_ = pkt.PermanentAccept()
			return true

		case 53:
			// Always allow direct access to the Portmaster Nameserver.
			// DNS is both UDP and TCP.

			// Check if a nameserver IP matcher is set.
			if !nameserverIPMatcherReady.IsSet() {
				return false
			}

			// Check if packet is destined for a nameserver IP.
			if !nameserverIPMatcher(meta.Dst) {
				return false
			}

			// Only fast-track local requests.
			isMe, err := netenv.IsMyIP(meta.Src)
			switch {
			case err != nil:
				log.Debugf("filter: failed to check if %s is own IP for fast-track: %s", meta.Src, err)
				return false
			case !isMe:
				return false
			}

			// Log and permit.
			log.Debugf("filter: fast-track accepting local dns: %s", pkt)
			_ = pkt.PermanentAccept()
			return true
		}
	}

	return false
}

func initialHandler(conn *network.Connection, pkt packet.Packet) {
	log.Tracer(pkt.Ctx()).Trace("filter: handing over to connection-based handler")

	// check for internal firewall bypass
	ps := getPortStatusAndMarkUsed(pkt.Info().LocalPort())
	if ps.isMe {
		// approve
		conn.Accept("connection by Portmaster", noReasonOptionKey)
		conn.Internal = true
		// finish
		conn.StopFirewallHandler()
		issueVerdict(conn, pkt, 0, true)
		return
	}

	// Redirect rogue dns requests to the Portmaster.
	if pkt.IsOutbound() &&
		pkt.Info().DstPort == 53 &&
		conn.Process().Pid != ownPID &&
		nameserverIPMatcherReady.IsSet() &&
		!nameserverIPMatcher(pkt.Info().Dst) {
		conn.Verdict = network.VerdictRerouteToNameserver
		conn.Reason.Msg = "redirecting rogue dns query"
		conn.Internal = true
		conn.StopFirewallHandler()
		issueVerdict(conn, pkt, 0, true)
		return
	}

	// check if filtering is enabled
	if !filterEnabled() {
		conn.Inspecting = false
		conn.Accept("privacy filter disabled", noReasonOptionKey)
		conn.StopFirewallHandler()
		issueVerdict(conn, pkt, 0, true)
		return
	}

	log.Tracer(pkt.Ctx()).Trace("filter: starting decision process")
	DecideOnConnection(pkt.Ctx(), conn, pkt)
	conn.Inspecting = false // TODO: enable inspecting again

	// tunneling
	// TODO: add implementation for forced tunneling
	if pkt.IsOutbound() &&
		captain.ClientReady() &&
		netutils.IPIsGlobal(conn.Entity.IP) &&
		conn.Verdict == network.VerdictAccept {
		// try to tunnel
		err := sluice.AwaitRequest(pkt.Info(), conn.Entity.Domain)
		if err != nil {
			log.Tracer(pkt.Ctx()).Tracef("filter: not tunneling: %s", err)
		} else {
			log.Tracer(pkt.Ctx()).Trace("filter: tunneling request")
			conn.Verdict = network.VerdictRerouteToTunnel
		}
	}

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
	// TODO: `pkt` has an active trace log, which we currently don't submit.
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
	case network.VerdictFailed:
		atomic.AddUint64(packetsFailed, 1)
		err = pkt.Drop()
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

func packetHandler(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case pkt := <-interception.Packets:
			interceptionModule.StartWorker("initial packet handler", func(workerCtx context.Context) error {
				handlePacket(workerCtx, pkt)
				return nil
			})
		}
	}
}

func statLogger(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
			log.Tracef(
				"filter: packets accepted %d, blocked %d, dropped %d, failed %d",
				atomic.LoadUint64(packetsAccepted),
				atomic.LoadUint64(packetsBlocked),
				atomic.LoadUint64(packetsDropped),
				atomic.LoadUint64(packetsFailed),
			)
			atomic.StoreUint64(packetsAccepted, 0)
			atomic.StoreUint64(packetsBlocked, 0)
			atomic.StoreUint64(packetsDropped, 0)
			atomic.StoreUint64(packetsFailed, 0)
		}
	}
}
