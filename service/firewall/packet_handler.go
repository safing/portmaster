package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/compat"
	_ "github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/firewall/inspection"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/portmaster/spn/access"
)

var (
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

func resetSingleConnectionVerdict(connID string) {
	// Create tracing context.
	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()

	conn, ok := network.GetConnection(connID)
	if !ok {
		conn, ok = network.GetDNSConnection(connID)
		if !ok {
			tracer.Debugf("filter: could not find re-attributed connection %s for re-evaluation", connID)
			return
		}
	}

	resetConnectionVerdict(ctx, conn)
}

func resetProfileConnectionVerdict(profileSource, profileID string) {
	// Create tracing context.
	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()

	// Resetting will force all the connection to be evaluated by the firewall again
	// this will set new verdicts if configuration was update or spn has been disabled or enabled.
	tracer.Infof("filter: re-evaluating connections of %s/%s", profileSource, profileID)

	// Re-evaluate all connections.
	var changedVerdicts int
	for _, conn := range network.GetAllConnections() {
		// Check if connection is complete and attributed to the deleted profile.
		if conn.DataIsComplete() &&
			conn.ProcessContext.Profile == profileID &&
			conn.ProcessContext.Source == profileSource {
			if resetConnectionVerdict(ctx, conn) {
				changedVerdicts++
			}
		}
	}
	tracer.Infof("filter: changed verdict on %d connections", changedVerdicts)
}

func resetAllConnectionVerdicts() {
	// Create tracing context.
	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()

	// Resetting will force all the connection to be evaluated by the firewall again
	// this will set new verdicts if configuration was update or spn has been disabled or enabled.
	tracer.Info("filter: re-evaluating all connections")

	// Re-evaluate all connections.
	var changedVerdicts int
	for _, conn := range network.GetAllConnections() {
		// Skip incomplete connections.
		if !conn.DataIsComplete() {
			continue
		}

		if resetConnectionVerdict(ctx, conn) {
			changedVerdicts++
		}
	}
	tracer.Infof("filter: changed verdict on %d connections", changedVerdicts)
}

func resetConnectionVerdict(ctx context.Context, conn *network.Connection) (verdictChanged bool) {
	tracer := log.Tracer(ctx)

	// Remove any active prompt as the settings are being re-evaluated.
	conn.RemovePrompt()

	conn.Lock()
	defer conn.Unlock()

	// Do not re-evaluate connection that have already ended.
	if conn.Ended > 0 {
		return false
	}

	// Update feature flags.
	if err := conn.UpdateFeatures(); err != nil && !errors.Is(err, access.ErrNotLoggedIn) {
		tracer.Warningf("filter: failed to update connection feature flags: %s", err)
	}

	// Skip internal connections:
	// - Pre-authenticated connections from Portmaster
	// - Redirected DNS requests
	// - SPN Uplink to Home Hub
	if conn.Internal {
		// tracer.Tracef("filter: skipping internal connection %s", conn)
		return false
	}

	tracer.Debugf("filter: re-evaluating verdict of %s", conn)
	previousVerdict := conn.Verdict

	// Apply privacy filter and check tunneling.
	FilterConnection(ctx, conn, nil, true, true)

	// Stop existing SPN tunnel if not needed anymore.
	if conn.Verdict != network.VerdictRerouteToTunnel && conn.TunnelContext != nil {
		err := conn.TunnelContext.StopTunnel()
		if err != nil {
			tracer.Debugf("filter: failed to stopped unneeded tunnel: %s", err)
		}
	}

	// Save if verdict changed.
	if conn.Verdict != previousVerdict {
		err := interception.UpdateVerdictOfConnection(conn)
		if err != nil {
			log.Debugf("filter: failed to update connection verdict: %s", err)
		}
		conn.Save()
		tracer.Infof("filter: verdict of connection %s changed from %s to %s", conn, previousVerdict.Verb(), conn.VerdictVerb())

		// Update verdict in OS integration, if an IP connection.
		if conn.Type == network.IPConnection {
			err := interception.UpdateVerdictOfConnection(conn)
			if err != nil {
				log.Debugf("filter: failed to update connection verdict: %s", err)
			}
		}

		return true
	}

	tracer.Tracef("filter: verdict to connection %s unchanged at %s", conn, conn.VerdictVerb())
	return false
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

func handlePacket(pkt packet.Packet) {
	// First, check for an existing connection.
	conn, ok := network.GetConnection(pkt.GetConnectionID())
	if ok {
		// Add packet to connection handler queue or apply verdict directly.
		conn.HandlePacket(pkt)
		return
	}

	// Else create new incomplete connection from the packet and start the new handler.
	conn = network.NewIncompleteConnection(pkt)
	conn.Lock()
	defer conn.Unlock()
	conn.SetFirewallHandler(fastTrackHandler)

	// Let the new connection handler worker handle the packet.
	conn.HandlePacket(pkt)
}

// fastTrackedPermit quickly permits certain network critical or internal connections.
func fastTrackedPermit(conn *network.Connection, pkt packet.Packet) (verdict network.Verdict, permanent bool) {
	meta := pkt.Info()

	// Check if packed was already fast-tracked by the OS integration.
	if pkt.FastTrackedByIntegration() {
		log.Tracer(pkt.Ctx()).Debugf("filter: fast-tracked by OS integration: %s", pkt)
		return network.VerdictAccept, true
	}

	// Check if connection was already blocked.
	if meta.Dst.Equal(blockedIPv4) || meta.Dst.Equal(blockedIPv6) {
		return network.VerdictBlock, true
	}

	// Some programs do a network self-check where they connects to the same
	// IP/Port to test network capabilities.
	// Eg. dig: https://gitlab.isc.org/isc-projects/bind9/-/issues/1140
	if meta.SrcPort == meta.DstPort &&
		meta.Src.Equal(meta.Dst) {
		log.Tracer(pkt.Ctx()).Debugf("filter: fast-track network self-check: %s", pkt)
		return network.VerdictAccept, true
	}

	switch meta.Protocol { //nolint:exhaustive // Checking for specific values only.
	case packet.ICMP, packet.ICMPv6:
		// Load packet data.
		err := pkt.LoadPacketData()
		if err != nil {
			log.Tracer(pkt.Ctx()).Debugf("filter: failed to load ICMP packet data: %s", err)
			return network.VerdictAccept, true
		}

		// Submit to ICMP listener.
		submitted := netenv.SubmitPacketToICMPListener(pkt)
		if submitted {
			// If the packet was submitted to the listener, we must not do a
			// permanent accept, because then we won't see any future packets of that
			// connection and thus cannot continue to submit them.
			log.Tracer(pkt.Ctx()).Debugf("filter: fast-track tracing ICMP/v6: %s", pkt)
			return network.VerdictAccept, false
		}

		// Handle echo request and replies regularly.
		// Other ICMP packets are considered system business.
		icmpLayers := pkt.Layers().LayerClass(layers.LayerClassIPControl)
		switch icmpLayer := icmpLayers.(type) {
		case *layers.ICMPv4:
			switch icmpLayer.TypeCode.Type() {
			case layers.ICMPv4TypeEchoRequest,
				layers.ICMPv4TypeEchoReply:
				return network.VerdictUndecided, false
			}
		case *layers.ICMPv6:
			switch icmpLayer.TypeCode.Type() {
			case layers.ICMPv6TypeEchoRequest,
				layers.ICMPv6TypeEchoReply:
				return network.VerdictUndecided, false
			}
		}

		// Permit all ICMP/v6 packets that are not echo requests or replies.
		log.Tracer(pkt.Ctx()).Debugf("filter: fast-track accepting ICMP/v6: %s", pkt)
		return network.VerdictAccept, true

	case packet.UDP, packet.TCP:
		switch meta.DstPort {

		case 67, 68, 546, 547:
			// Always allow DHCP, DHCPv6.

			// DHCP and DHCPv6 must be UDP.
			if meta.Protocol != packet.UDP {
				return network.VerdictUndecided, false
			}

			// DHCP is only valid in local network scopes.
			switch netutils.ClassifyIP(meta.Dst) { //nolint:exhaustive // Checking for specific values only.
			case netutils.HostLocal, netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			default:
				return network.VerdictUndecided, false
			}

			// Log and permit.
			log.Tracer(pkt.Ctx()).Debugf("filter: fast-track accepting DHCP: %s", pkt)
			return network.VerdictAccept, true

		case apiPort:
			// Always allow direct access to the Portmaster API.

			// Portmaster API is TCP only.
			if meta.Protocol != packet.TCP {
				return network.VerdictUndecided, false
			}

			// Check if the api port is even set.
			if !apiPortSet {
				return network.VerdictUndecided, false
			}

			// Must be destined for the API IP.
			if !meta.Dst.Equal(apiIP) {
				return network.VerdictUndecided, false
			}

			// Only fast-track local requests.
			isMe, err := netenv.IsMyIP(meta.Src)
			switch {
			case err != nil:
				log.Tracer(pkt.Ctx()).Debugf("filter: failed to check if %s is own IP for fast-track: %s", meta.Src, err)
				return network.VerdictUndecided, false
			case !isMe:
				return network.VerdictUndecided, false
			}

			// Log and permit.
			log.Tracer(pkt.Ctx()).Debugf("filter: fast-track accepting api connection: %s", pkt)
			return network.VerdictAccept, true

		case 53:
			// Always allow direct access to the Portmaster Nameserver.
			// DNS is both UDP and TCP.

			// Check if a nameserver IP matcher is set.
			if !nameserverIPMatcherReady.IsSet() {
				return network.VerdictUndecided, false
			}

			// Check if packet is destined for a nameserver IP.
			if !nameserverIPMatcher(meta.Dst) {
				return network.VerdictUndecided, false
			}

			// Only fast-track local requests.
			isMe, err := netenv.IsMyIP(meta.Src)
			switch {
			case err != nil:
				log.Tracer(pkt.Ctx()).Debugf("filter: failed to check if %s is own IP for fast-track: %s", meta.Src, err)
				return network.VerdictUndecided, false
			case !isMe:
				return network.VerdictUndecided, false
			}

			// Log and permit.
			log.Tracer(pkt.Ctx()).Debugf("filter: fast-track accepting local dns: %s", pkt)

			// Add to DNS request connections to attribute DNS request if outgoing.
			if pkt.IsOutbound() {
				// Assign PID from packet directly, as processing stops after fast-track.
				conn.PID = pkt.Info().PID
				network.SaveDNSRequestConnection(conn, pkt)
			}

			// Accept local DNS, but only make permanent if we have the PID too.
			return network.VerdictAccept, conn.PID != process.UndefinedProcessID
		}

	case compat.SystemIntegrationCheckProtocol:
		if pkt.Info().Dst.Equal(compat.SystemIntegrationCheckDstIP) {
			compat.SubmitSystemIntegrationCheckPacket(pkt)
			return network.VerdictDrop, false
		}
	}

	return network.VerdictUndecided, false
}

func fastTrackHandler(conn *network.Connection, pkt packet.Packet) {
	conn.SaveWhenFinished()

	fastTrackedVerdict, permanent := fastTrackedPermit(conn, pkt)
	if fastTrackedVerdict != network.VerdictUndecided {
		// Set verdict on connection.
		conn.Verdict = fastTrackedVerdict

		// Apply verdict to (real) packet.
		if !pkt.InfoOnly() {
			issueVerdict(conn, pkt, fastTrackedVerdict, permanent)
		}

		// Stop handler if permanent.
		if permanent {
			conn.SetVerdict(fastTrackedVerdict, "fast-tracked", "", nil)

			// Do not finalize verdict, as we are missing necessary data.
			conn.StopFirewallHandler()
		}

		// Do not continue to next handler.
		return
	}

	// If packet is not fast-tracked, continue with gathering more information.
	conn.UpdateFirewallHandler(gatherDataHandler)
	gatherDataHandler(conn, pkt)
}

func gatherDataHandler(conn *network.Connection, pkt packet.Packet) {
	conn.SaveWhenFinished()

	// Get process info
	_ = conn.GatherConnectionInfo(pkt)
	// Errors are informational and are logged to the context.

	// Run this handler again if data is not yet complete.
	if !conn.DataIsComplete() {
		return
	}

	// Continue to filter handler, when connection data is complete.
	switch conn.IPProtocol { //nolint:exhaustive
	case packet.ICMP, packet.ICMPv6:
		conn.UpdateFirewallHandler(icmpFilterHandler)
		icmpFilterHandler(conn, pkt)

	default:
		conn.UpdateFirewallHandler(filterHandler)
		filterHandler(conn, pkt)
	}
}

func filterHandler(conn *network.Connection, pkt packet.Packet) {
	conn.SaveWhenFinished()

	// Skip if data is not complete or packet is info-only.
	if !conn.DataIsComplete() || pkt.InfoOnly() {
		return
	}

	filterConnection := true

	// Check for special (internal) connection cases.
	switch {
	case !conn.Inbound && localPortIsPreAuthenticated(conn.Entity.Protocol, conn.LocalPort):
		// Approve connection.
		conn.Accept("connection by Portmaster", noReasonOptionKey)
		conn.Internal = true
		filterConnection = false
		log.Tracer(pkt.Ctx()).Infof("filter: granting own pre-authenticated connection %s", conn)

	// Redirect outbound DNS packets if enabled,
	case dnsQueryInterception() &&
		!module.instance.Resolver().IsDisabled() &&
		pkt.IsOutbound() &&
		pkt.Info().DstPort == 53 &&
		// that don't match the address of our nameserver,
		nameserverIPMatcherReady.IsSet() &&
		!nameserverIPMatcher(pkt.Info().Dst) &&
		// and are not broadcast queries by us.
		// Context:
		// - Unicast queries by the resolver are pre-authenticated.
		// - Unicast queries by the compat self-check should be redirected.
		!(conn.Process().Pid == ownPID &&
			conn.Entity.IPScope == netutils.LocalMulticast):

		// Reroute rogue dns queries back to Portmaster.
		conn.SetVerdict(network.VerdictRerouteToNameserver, "redirecting rogue dns query", "", nil)
		conn.Internal = true
		log.Tracer(pkt.Ctx()).Infof("filter: redirecting dns query %s to Portmaster", conn)

		// Add to DNS request connections to attribute DNS request.
		network.SaveDNSRequestConnection(conn, pkt)

		// End directly, as no other processing is necessary.
		conn.StopFirewallHandler()

		issueVerdict(conn, pkt, 0, true)
		return
	}

	// Apply privacy filter and check tunneling.
	FilterConnection(pkt.Ctx(), conn, pkt, filterConnection, true)

	// Decide how to continue handling connection.
	switch {
	case conn.Inspecting && looksLikeOutgoingDNSRequest(conn):
		inspectDNSPacket(conn, pkt)
		conn.UpdateFirewallHandler(inspectDNSPacket)
	case conn.Inspecting:
		log.Tracer(pkt.Ctx()).Trace("filter: start inspecting")
		conn.UpdateFirewallHandler(inspectAndVerdictHandler)
		inspectAndVerdictHandler(conn, pkt)
	default:
		conn.StopFirewallHandler()
		verdictHandler(conn, pkt)
	}
}

// FilterConnection runs all the filtering (and tunneling) procedures.
func FilterConnection(ctx context.Context, conn *network.Connection, pkt packet.Packet, checkFilter, checkTunnel bool) {
	// Skip if data is not complete.
	if !conn.DataIsComplete() {
		return
	}

	if checkFilter {
		if filterEnabled() {
			log.Tracer(ctx).Trace("filter: starting decision process")
			decideOnConnection(ctx, conn, pkt)
		} else {
			conn.Accept("privacy filter disabled", noReasonOptionKey)
		}
	}

	// TODO: Enable inspection framework again.
	// conn.Inspecting = false

	// TODO: Quick fix for the SPN.
	// Use inspection framework for proper encryption detection.
	switch conn.Entity.DstPort() {
	case
		22,  // SSH
		443, // HTTPS
		465, // SMTP-SSL
		853, // DoT
		993, // IMAP-SSL
		995: // POP3-SSL
		conn.Encrypted = true
	}

	// Check if connection should be tunneled.
	if checkTunnel {
		checkTunneling(ctx, conn)
	}

	// Request tunneling if no tunnel is set and connection should be tunneled.
	if conn.Verdict == network.VerdictRerouteToTunnel &&
		conn.TunnelContext == nil {
		err := requestTunneling(ctx, conn)
		if err == nil {
			conn.ConnectionEstablished = true
		} else {
			// Set connection to failed, but keep tunneling data.
			// The tunneling data makes connection easy to recognize as a failed SPN
			// connection and the data will help with debugging and displaying in the UI.
			conn.Failed(fmt.Sprintf("failed to request tunneling: %s", err), "")
		}
	}
}

// defaultFirewallHandler is used when no other firewall handler is set on a connection.
func defaultFirewallHandler(conn *network.Connection, pkt packet.Packet) {
	switch conn.IPProtocol { //nolint:exhaustive
	case packet.ICMP, packet.ICMPv6:
		// Always use the ICMP handler for ICMP connections.
		icmpFilterHandler(conn, pkt)

	default:
		verdictHandler(conn, pkt)
	}
}

func verdictHandler(conn *network.Connection, pkt packet.Packet) {
	// Ignore info-only packets in this handler.
	if pkt.InfoOnly() {
		return
	}

	issueVerdict(conn, pkt, 0, true)
}

func inspectAndVerdictHandler(conn *network.Connection, pkt packet.Packet) {
	// Ignore info-only packets in this handler.
	if pkt.InfoOnly() {
		return
	}

	// Run inspectors.
	pktVerdict, continueInspection := inspection.RunInspectors(conn, pkt)
	if continueInspection {
		issueVerdict(conn, pkt, pktVerdict, false)
		return
	}

	// we are done with inspecting
	conn.StopFirewallHandler()
	issueVerdict(conn, pkt, 0, true)
}

func inspectDNSPacket(conn *network.Connection, pkt packet.Packet) {
	// Ignore info-only packets in this handler.
	if pkt.InfoOnly() {
		return
	}

	dnsPacket := new(dns.Msg)
	err := pkt.LoadPacketData()
	if err != nil {
		_ = pkt.Block()
		log.Errorf("filter: failed to load packet payload: %s", err)
		return
	}

	// Parse and block invalid packets.
	err = dnsPacket.Unpack(pkt.Payload())
	if err != nil {
		err = pkt.PermanentBlock()
		if err != nil {
			log.Errorf("filter: failed to block packet: %s", err)
		}
		_ = conn.SetVerdict(network.VerdictBlock, "none DNS data on DNS port", "", nil)
		conn.VerdictPermanent = true
		conn.Save()
		return
	}

	// Packet was parsed.
	// Allow it but only after the answer was added to the cache.
	defer func() {
		err = pkt.Accept()
		if err != nil {
			log.Errorf("filter: failed to accept dns packet: %s", err)
		}
	}()

	// Check if packet has a question.
	if len(dnsPacket.Question) == 0 {
		return
	}

	// Read create structs with the needed data.
	question := dnsPacket.Question[0]
	fqdn := dns.Fqdn(question.Name)

	// Check for compat check dns request.
	if strings.HasSuffix(fqdn, compat.DNSCheckInternalDomainScope) {
		subdomain := strings.TrimSuffix(fqdn, compat.DNSCheckInternalDomainScope)
		_ = compat.SubmitDNSCheckDomain(subdomain)
		log.Infof("packet_handler: self-check domain received")
		// No need to parse the answer.
		return
	}

	// Check if there is an answer.
	if len(dnsPacket.Answer) == 0 {
		return
	}

	resolverInfo := &resolver.ResolverInfo{
		Name:    "DNSRequestObserver",
		Type:    resolver.ServerTypeFirewall,
		Source:  resolver.ServerSourceFirewall,
		IP:      conn.Entity.IP,
		Domain:  conn.Entity.Domain,
		IPScope: conn.Entity.IPScope,
	}

	rrCache := &resolver.RRCache{
		Domain:   fqdn,
		Question: dns.Type(question.Qtype),
		RCode:    dnsPacket.Rcode,
		Answer:   dnsPacket.Answer,
		Ns:       dnsPacket.Ns,
		Extra:    dnsPacket.Extra,
		Resolver: resolverInfo,
	}

	query := &resolver.Query{
		FQDN:               fqdn,
		QType:              dns.Type(question.Qtype),
		NoCaching:          false,
		IgnoreFailing:      false,
		LocalResolversOnly: false,
		ICANNSpace:         false,
		DomainRoot:         "",
	}

	// Save to cache
	UpdateIPsAndCNAMEs(query, rrCache, conn)
}

func icmpFilterHandler(conn *network.Connection, pkt packet.Packet) {
	// Load packet data.
	err := pkt.LoadPacketData()
	if err != nil {
		log.Tracer(pkt.Ctx()).Debugf("filter: failed to load ICMP packet data: %s", err)
		issueVerdict(conn, pkt, network.VerdictDrop, false)
		return
	}

	// Submit to ICMP listener.
	submitted := netenv.SubmitPacketToICMPListener(pkt)
	if submitted {
		issueVerdict(conn, pkt, network.VerdictDrop, false)
		return
	}

	// Handle echo request and replies regularly.
	// Other ICMP packets are considered system business.
	icmpLayers := pkt.Layers().LayerClass(layers.LayerClassIPControl)
	switch icmpLayer := icmpLayers.(type) {
	case *layers.ICMPv4:
		switch icmpLayer.TypeCode.Type() {
		case layers.ICMPv4TypeEchoRequest,
			layers.ICMPv4TypeEchoReply:
			// Continue
		default:
			issueVerdict(conn, pkt, network.VerdictAccept, false)
			return
		}

	case *layers.ICMPv6:
		switch icmpLayer.TypeCode.Type() {
		case layers.ICMPv6TypeEchoRequest,
			layers.ICMPv6TypeEchoReply:
			// Continue

		default:
			issueVerdict(conn, pkt, network.VerdictAccept, false)
			return
		}
	}

	// Check if we already have a verdict.
	switch conn.Verdict { //nolint:exhaustive
	case network.VerdictUndecided, network.VerdictUndeterminable:
		// Apply privacy filter and check tunneling.
		FilterConnection(pkt.Ctx(), conn, pkt, true, false)

		// Save and propagate changes.
		conn.SaveWhenFinished()
	}

	// Outbound direction has priority.
	if conn.Inbound && conn.Ended == 0 && pkt.IsOutbound() {
		// Change direction from inbound to outbound on first outbound ICMP packet.
		conn.Inbound = false

		// Apply privacy filter and check tunneling.
		FilterConnection(pkt.Ctx(), conn, pkt, true, false)

		// Save and propagate changes.
		conn.SaveWhenFinished()
	}

	issueVerdict(conn, pkt, 0, false)
}

func issueVerdict(conn *network.Connection, pkt packet.Packet, verdict network.Verdict, allowPermanent bool) {
	// Check if packed was already fast-tracked by the OS integration.
	if pkt.FastTrackedByIntegration() {
		return
	}

	// Enable permanent verdict.
	if allowPermanent && !conn.VerdictPermanent && permanentVerdicts() {
		conn.VerdictPermanent = true
		conn.SaveWhenFinished()
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
	case network.VerdictUndecided, network.VerdictUndeterminable:
		log.Tracer(pkt.Ctx()).Warningf("filter: tried to apply verdict %s to pkt %s: dropping instead", verdict, pkt)
		fallthrough
	default:
		atomic.AddUint64(packetsDropped, 1)
		err = pkt.Drop()
	}

	if err != nil {
		log.Tracer(pkt.Ctx()).Warningf("filter: failed to apply verdict to pkt %s: %s", pkt, err)
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

func packetHandler(w *mgr.WorkerCtx) error {
	for {
		select {
		case <-w.Done():
			return nil
		case pkt := <-interception.Packets:
			if pkt != nil {
				handlePacket(pkt)
			} else {
				return errors.New("received nil packet from interception")
			}
		}
	}
}

func bandwidthUpdateHandler(w *mgr.WorkerCtx) error {
	for {
		select {
		case <-w.Done():
			return nil
		case bwUpdate := <-interception.BandwidthUpdates:
			if bwUpdate != nil {
				// DEBUG:
				// log.Debugf("filter: bandwidth update: %s", bwUpdate)
				updateBandwidth(w.Ctx(), bwUpdate)
			} else {
				return errors.New("received nil bandwidth update from interception")
			}
		}
	}
}

func updateBandwidth(ctx context.Context, bwUpdate *packet.BandwidthUpdate) {
	// Check if update makes sense.
	if bwUpdate.BytesReceived == 0 && bwUpdate.BytesSent == 0 {
		return
	}

	// Get connection.
	conn, ok := network.GetConnection(bwUpdate.ConnID)
	if !ok {
		return
	}

	// Do not wait for connections that are locked.
	// TODO: Use atomic operations for updating bandwidth stats.
	if !conn.TryLock() {
		// DEBUG:
		// log.Warningf("filter: failed to lock connection for bandwidth update: %s", conn)
		return
	}
	defer conn.Unlock()

	bytesIn := bwUpdate.BytesReceived
	bytesOut := bwUpdate.BytesSent

	// Update stats according to method.
	switch bwUpdate.Method {
	case packet.Absolute:
		bytesIn = bwUpdate.BytesReceived - conn.BytesReceived
		bytesOut = bwUpdate.BytesSent - conn.BytesSent

		conn.BytesReceived = bwUpdate.BytesReceived
		conn.BytesSent = bwUpdate.BytesSent
	case packet.Additive:
		conn.BytesReceived += bwUpdate.BytesReceived
		conn.BytesSent += bwUpdate.BytesSent
	default:
		log.Warningf("filter: unsupported bandwidth update method: %d", bwUpdate.Method)
		return
	}

	// Update bandwidth in the netquery module.
	if module.instance.NetQuery() != nil && conn.BandwidthEnabled {
		if err := module.instance.NetQuery().Store.UpdateBandwidth(
			ctx,
			conn.HistoryEnabled,
			fmt.Sprintf("%s/%s", conn.ProcessContext.Source, conn.ProcessContext.Profile),
			conn.Process().GetKey(),
			conn.ID,
			bytesIn,
			bytesOut,
		); err != nil {
			log.Errorf("filter: failed to persist bandwidth data: %s", err)
		}
	}
}

func statLogger(w *mgr.WorkerCtx) error {
	for {
		select {
		case <-w.Done():
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
