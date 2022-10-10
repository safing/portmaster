package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/tevino/abool"
	"golang.org/x/sync/singleflight"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/compat"
	_ "github.com/safing/portmaster/core/base"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/firewall/interception"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/reference"
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

const (
	configChangeEvent        = "config change"
	profileConfigChangeEvent = "profile config change"
	onSPNConnectEvent        = "spn connect"
)

func init() {
	// TODO: Move interception module to own package (dir).
	interceptionModule = modules.Register("interception", interceptionPrep, interceptionStart, interceptionStop, "base", "updates", "network", "notifications", "profiles")

	network.SetDefaultFirewallHandler(defaultHandler)
}

func interceptionPrep() error {
	// Reset connections every time configuration changes
	// this will be triggered on spn enable/disable
	err := interceptionModule.RegisterEventHook(
		"config",
		configChangeEvent,
		"reset connection verdicts",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("interception: failed registering event hook: %s", err)
	}

	// Reset connections every time profile changes
	err = interceptionModule.RegisterEventHook(
		"profiles",
		profileConfigChangeEvent,
		"reset connection verdicts",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("failed registering event hook: %s", err)
	}

	// Reset connections when spn is connected
	// connect and disconnecting is triggered on config change event but connecting takеs more time
	err = interceptionModule.RegisterEventHook(
		"captain",
		onSPNConnectEvent,
		"reset connection verdicts",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("failed registering event hook: %s", err)
	}

	if err := registerConfig(); err != nil {
		return err
	}

	return prepAPIAuth()
}

func resetAllConnectionVerdicts() {
	// Resetting will force all the connection to be evaluated by the firewall again
	// this will set new verdicts if configuration was update or spn has been disabled or enabled.
	log.Info("interception: marking all connections for re-evaluation")

	// Create tracing context.
	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()

	// Re-evaluate all connections.
	for _, conn := range network.GetAllConnections() {
		func() {
			conn.Lock()
			defer conn.Unlock()

			// Skip internal connections:
			// - Pre-authenticated connections from Portmaster
			// - Redirected DNS requests
			// - SPN Uplink to Home Hub
			if conn.Internal {
				log.Tracef("skipping internal connection %s", conn)
				return
			}

			log.Tracer(ctx).Debugf("filter: re-evaluating verdict of %s", conn)
			previousVerdict := conn.Verdict.Firewall

			// Apply privacy filter and check tunneling.
			filterConnection(ctx, conn, nil)

			// Stop existing SPN tunnel if not needed anymore.
			if conn.Verdict.Active != network.VerdictRerouteToTunnel && conn.TunnelContext != nil {
				err := conn.TunnelContext.StopTunnel()
				if err != nil {
					log.Debugf("filter: failed to stopped unneeded tunnel: %s", err)
				}
			}

			// Save if verdict changed.
			if conn.Verdict.Firewall != previousVerdict {
				conn.Save()
				tracer.Infof("filter: verdict of connection %s changed from %s to %s", conn, previousVerdict.Verb(), conn.VerdictVerb())
			} else {
				tracer.Tracef("filter: verdict to connection %s unchanged at %s", conn, conn.VerdictVerb())
			}
		}()
	}

	err := interception.ResetVerdictOfAllConnections()
	if err != nil {
		log.Errorf("interception: failed to remove persistent verdicts: %s", err)
	}
}

func interceptionStart() error {
	getConfig()

	if err := registerMetrics(); err != nil {
		return err
	}

	startAPIAuth()

	interceptionModule.StartWorker("stat logger", statLogger)
	interceptionModule.StartWorker("packet handler", packetHandler)

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
	// Record metrics.
	startTime := time.Now()
	defer packetHandlingHistogram.UpdateDuration(startTime)

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

	// Get connection of packet.
	conn, err := getConnection(pkt)
	if err != nil {
		tracer.Errorf("filter: packet %s dropped: %s", pkt, err)
		_ = pkt.Drop()
		return
	}

	// handle packet
	conn.HandlePacket(pkt)
}

var getConnectionSingleInflight singleflight.Group

func getConnection(pkt packet.Packet) (*network.Connection, error) {
	created := false

	// Create or get connection in single inflight lock in order to prevent duplicates.
	newConn, err, shared := getConnectionSingleInflight.Do(pkt.GetConnectionID(), func() (interface{}, error) {
		// First, check for an existing connection.
		conn, ok := network.GetConnection(pkt.GetConnectionID())
		if ok {
			return conn, nil
		}

		// Else create new one from the packet.
		conn = network.NewConnectionFromFirstPacket(pkt)
		conn.Lock()
		defer conn.Unlock()
		conn.SetFirewallHandler(initialHandler)
		created = true
		return conn, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	if newConn == nil {
		return nil, errors.New("connection getter returned nil")
	}

	// Transform and log result.
	conn := newConn.(*network.Connection) //nolint:forcetypeassert // Can only be a *network.Connection.
	sharedIndicator := ""
	if shared {
		sharedIndicator = " (shared)"
	}
	if created {
		log.Tracer(pkt.Ctx()).Tracef("filter: created new connection %s%s", conn.ID, sharedIndicator)
	} else {
		log.Tracer(pkt.Ctx()).Tracef("filter: assigned connection %s%s", conn.ID, sharedIndicator)
	}

	return conn, nil
}

// fastTrackedPermit quickly permits certain network critical or internal connections.
func fastTrackedPermit(pkt packet.Packet) (handled bool) {
	meta := pkt.Info()

	// Check if packed was already fast-tracked by the OS integration.
	if pkt.FastTrackedByIntegration() {
		log.Debugf("filter: fast-tracked by OS integration: %s", pkt)
		return true
	}

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

	switch meta.Protocol { //nolint:exhaustive // Checking for specific values only.
	case packet.ICMP, packet.ICMPv6:
		// Load packet data.
		err := pkt.LoadPacketData()
		if err != nil {
			log.Debugf("filter: failed to load ICMP packet data: %s", err)
			_ = pkt.PermanentAccept()
			return true
		}

		// Submit to ICMP listener.
		submitted := netenv.SubmitPacketToICMPListener(pkt)
		if submitted {
			// If the packet was submitted to the listener, we must not do a
			// permanent accept, because then we won't see any future packets of that
			// connection and thus cannot continue to submit them.
			log.Debugf("filter: fast-track tracing ICMP/v6: %s", pkt)
			_ = pkt.Accept()
			return true
		}

		// Handle echo request and replies regularly.
		// Other ICMP packets are considered system business.
		icmpLayers := pkt.Layers().LayerClass(layers.LayerClassIPControl)
		switch icmpLayer := icmpLayers.(type) {
		case *layers.ICMPv4:
			switch icmpLayer.TypeCode.Type() {
			case layers.ICMPv4TypeEchoRequest,
				layers.ICMPv4TypeEchoReply:
				return false
			}
		case *layers.ICMPv6:
			switch icmpLayer.TypeCode.Type() {
			case layers.ICMPv6TypeEchoRequest,
				layers.ICMPv6TypeEchoReply:
				return false
			}
		}

		// Permit all ICMP/v6 packets that are not echo requests or replies.
		log.Debugf("filter: fast-track accepting ICMP/v6: %s", pkt)
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
			switch netutils.ClassifyIP(meta.Dst) { //nolint:exhaustive // Checking for specific values only.
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

	case compat.SystemIntegrationCheckProtocol:
		if pkt.Info().Dst.Equal(compat.SystemIntegrationCheckDstIP) {
			compat.SubmitSystemIntegrationCheckPacket(pkt)
			_ = pkt.Drop()
			return true
		}
	}

	return false
}

func initialHandler(conn *network.Connection, pkt packet.Packet) {
	log.Tracer(pkt.Ctx()).Trace("filter: handing over to connection-based handler")

	// Check for special (internal) connection cases.
	switch {
	case !conn.Inbound && localPortIsPreAuthenticated(conn.Entity.Protocol, conn.LocalPort):
		// Approve connection.
		conn.Accept("connection by Portmaster", noReasonOptionKey)
		conn.Internal = true

		// Redirect outbound DNS packets if enabled,
	case dnsQueryInterception() &&
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
		conn.SetVerdictDirectly(network.VerdictRerouteToNameserver)
		conn.Reason.Msg = "redirecting rogue dns query"
		conn.Internal = true
		// End directly, as no other processing is necessary.
		conn.StopFirewallHandler()
		finalizeVerdict(conn)
		issueVerdict(conn, pkt, 0, true)
		return
	}

	// Apply privacy filter and check tunneling.
	filterConnection(pkt.Ctx(), conn, pkt)

	// Decide how to continue handling connection.
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

func filterConnection(ctx context.Context, conn *network.Connection, pkt packet.Packet) {
	if filterEnabled() {
		log.Tracer(ctx).Trace("filter: starting decision process")
		DecideOnConnection(ctx, conn, pkt)
	} else {
		conn.Accept("privacy filter disabled", noReasonOptionKey)
	}

	// TODO: Enable inspection framework again.
	conn.Inspecting = false

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
	checkTunneling(ctx, conn)

	// Handle verdict records and transitions.
	finalizeVerdict(conn)

	// Request tunneling if no tunnel is set and connection should be tunneled.
	if conn.Verdict.Active == network.VerdictRerouteToTunnel &&
		conn.TunnelContext == nil {
		err := requestTunneling(ctx, conn)
		if err != nil {
			// Set connection to failed, but keep tunneling data.
			// The tunneling data makes connection easy to recognize as a failed SPN
			// connection and the data will help with debugging and displaying in the UI.
			conn.Failed(fmt.Sprintf("failed to request tunneling: %s", err), "")
			finalizeVerdict(conn)
		}
	}

	// Make sure we update the worst verdict after the final verdict is set by the firewall
	updateConnectionWorstVerdict(conn)
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
	if verdict < conn.Verdict.Active {
		verdict = conn.Verdict.Active
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
		log.Warningf("filter: tried to apply verdict %s to pkt %s: dropping instead", verdict, pkt)
		fallthrough
	default:
		atomic.AddUint64(packetsDropped, 1)
		err = pkt.Drop()
	}

	if err != nil {
		log.Warningf("filter: failed to apply verdict to pkt %s: %s", pkt, err)
	}
}

func finalizeVerdict(conn *network.Connection) {
	// Check for non-applicable verdicts.
	// The earlier and clearer we do this, the better.
	switch conn.Verdict.Firewall { //nolint:exhaustive
	case network.VerdictUndecided, network.VerdictUndeterminable, network.VerdictFailed:
		if conn.Inbound {
			conn.Verdict.Active = network.VerdictDrop
		} else {
			conn.Verdict.Active = network.VerdictBlock
		}
		return
	}

	// Apply firewall verdict to active verdict.
	switch {
	case conn.Verdict.Active == network.VerdictUndecided:
		// Apply first verdict without change.
		conn.Verdict.Active = conn.Verdict.Firewall

	case conn.Verdict.Worst == network.VerdictBlock ||
		conn.Verdict.Worst == network.VerdictDrop ||
		conn.Verdict.Worst == network.VerdictFailed ||
		conn.Verdict.Worst == network.VerdictUndeterminable:
		// Always allow to change verdict from any real initial/worst non-allowed state.
		// Note: This check needs to happen before updating the Worst verdict.
		conn.Verdict.Active = conn.Verdict.Firewall

	case reference.IsPacketProtocol(conn.Entity.Protocol):
		// For known packet protocols, apply firewall verdict unchanged.
		conn.Verdict.Active = conn.Verdict.Firewall

	case conn.Verdict.Active != conn.Verdict.Firewall:
		// For all other protocols (most notably, stream protocols), always block after the first change.
		// Block in both directions, as there is a live connection, which we want to actively kill.
		conn.Verdict.Active = network.VerdictBlock
	}
}

// verdictRating rates the privacy and security aspect of verdicts from worst to best.
var verdictRating = []network.Verdict{
	network.VerdictAccept,              // Connection allowed in the open.
	network.VerdictRerouteToTunnel,     // Connection allowed, but protected.
	network.VerdictRerouteToNameserver, // Connection allowed, but resolved via Portmaster.
	network.VerdictBlock,               // Connection blocked, with feedback.
	network.VerdictDrop,                // Connection blocked, without feedback.
	network.VerdictFailed,
	network.VerdictUndeterminable,
	network.VerdictUndecided,
}

func updateConnectionWorstVerdict(conn *network.Connection) {
	for _, worstVerdict := range verdictRating {
		if conn.Verdict.Firewall == worstVerdict {
			conn.Verdict.Worst = worstVerdict
		}
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
