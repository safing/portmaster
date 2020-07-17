package firewall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/portmaster/netenv"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/state"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/endpoints"

	"github.com/agext/levenshtein"
)

// Call order:
//
// DNS Query:
// 1. DecideOnConnection
//    is called when a DNS query is made, may set verdict to Undeterminable to permit a DNS reply.
//    is called with a nil packet.
// 2. DecideOnResolvedDNS
//    is called to (possibly) filter out A/AAAA records that the filter would deny later.
//
// Network Connection:
// 3. DecideOnConnection
//    is called with the first packet of a network connection.

// DecideOnConnection makes a decision about a connection.
// When called, the connection and profile is already locked.
func DecideOnConnection(ctx context.Context, conn *network.Connection, pkt packet.Packet) {
	// update profiles and check if communication needs reevaluation
	if conn.UpdateAndCheck() {
		log.Tracer(ctx).Infof("filter: re-evaluating verdict on %s", conn)
		conn.Verdict = network.VerdictUndecided

		if conn.Entity != nil {
			conn.Entity.ResetLists()
		}
	}

	var deciders = []func(context.Context, *network.Connection, packet.Packet) bool{
		checkPortmasterConnection,
		checkSelfCommunication,
		checkProfileExists,
		checkConnectionType,
		checkConnectivityDomain,
		checkConnectionScope,
		checkEndpointLists,
		checkBypassPrevention,
		checkFilterLists,
		checkInbound,
		checkDefaultPermit,
		checkAutoPermitRelated,
		checkDefaultAction,
	}

	for _, decider := range deciders {
		if decider(ctx, conn, pkt) {
			return
		}
	}

	// DefaultAction == DefaultActionBlock
	conn.Deny("endpoint is not whitelisted (default=block)")
}

// checkPortmasterConnection allows all connection that originate from
// portmaster itself.
func checkPortmasterConnection(ctx context.Context, conn *network.Connection, pkt packet.Packet) bool {
	// grant self
	if conn.Process().Pid == os.Getpid() {
		log.Tracer(ctx).Infof("filter: granting own connection %s", conn)
		conn.Verdict = network.VerdictAccept
		conn.Internal = true
		return true
	}

	return false
}

// checkSelfCommunication checks if the process is communicating with itself.
func checkSelfCommunication(ctx context.Context, conn *network.Connection, pkt packet.Packet) bool {
	// check if process is communicating with itself
	if pkt != nil {
		// TODO: evaluate the case where different IPs in the 127/8 net are used.
		pktInfo := pkt.Info()
		if conn.Process().Pid >= 0 && pktInfo.Src.Equal(pktInfo.Dst) {
			// get PID
			otherPid, _, err := state.Lookup(&packet.Info{
				Inbound:  !pktInfo.Inbound, // we want to know the process on the other end
				Version:  pktInfo.Version,
				Protocol: pktInfo.Protocol,
				Src:      pktInfo.Src,
				SrcPort:  pktInfo.SrcPort,
				Dst:      pktInfo.Dst,
				DstPort:  pktInfo.DstPort,
			})
			if err != nil {
				log.Tracer(ctx).Warningf("filter: failed to find local peer process PID: %s", err)
			} else {
				// get primary process
				otherProcess, err := process.GetOrFindPrimaryProcess(ctx, otherPid)
				if err != nil {
					log.Tracer(ctx).Warningf("filter: failed to find load local peer process with PID %d: %s", otherPid, err)
				} else if otherProcess.Pid == conn.Process().Pid {
					conn.Accept("connection to self")
					conn.Internal = true
					return true
				}
			}
		}
	}

	return false
}

func checkProfileExists(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	if conn.Process().Profile() == nil {
		conn.Block("unknown process or profile")
		return true
	}
	return false
}

func checkEndpointLists(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	var result endpoints.EPResult
	var reason endpoints.Reason

	// there must always be a profile.
	p := conn.Process().Profile()

	// check endpoints list
	if conn.Inbound {
		result, reason = p.MatchServiceEndpoint(conn.Entity)
	} else {
		result, reason = p.MatchEndpoint(conn.Entity)
	}
	switch result {
	case endpoints.Denied:
		conn.DenyWithContext(reason.String(), reason.Context())
		return true
	case endpoints.Permitted:
		conn.AcceptWithContext(reason.String(), reason.Context())
		return true
	}

	return false
}

func checkConnectionType(ctx context.Context, conn *network.Connection, _ packet.Packet) bool {
	p := conn.Process().Profile()

	// check conn type
	switch conn.Scope {
	case network.IncomingLAN, network.IncomingInternet, network.IncomingInvalid:
		if p.BlockInbound() {
			if conn.Scope == network.IncomingHost {
				conn.Block("inbound connections blocked")
			} else {
				conn.Drop("inbound connections blocked")
			}
			return true
		}
	case network.PeerInternet:
		// BlockP2P only applies to connections to the Internet
		if p.BlockP2P() {
			conn.Block("direct connections (P2P) blocked")
			return true
		}
	}

	return false
}

func checkConnectivityDomain(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	p := conn.Process().Profile()

	if !p.BlockScopeInternet() {
		// Special grant only applies if application is allowed to connect to the Internet.
		return false
	}

	if netenv.GetOnlineStatus() <= netenv.StatusPortal &&
		netenv.IsConnectivityDomain(conn.Entity.Domain) {
		conn.Accept("special grant for connectivity domain during network bootstrap")
		return true
	}

	return false
}

func checkConnectionScope(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	p := conn.Process().Profile()

	// check scopes
	if conn.Entity.IP != nil {
		classification := netutils.ClassifyIP(conn.Entity.IP)

		switch classification {
		case netutils.Global, netutils.GlobalMulticast:
			if p.BlockScopeInternet() {
				conn.Deny("Internet access blocked") // Block Outbound / Drop Inbound
				return true
			}
		case netutils.SiteLocal, netutils.LinkLocal, netutils.LocalMulticast:
			if p.BlockScopeLAN() {
				conn.Block("LAN access blocked") // Block Outbound / Drop Inbound
				return true
			}
		case netutils.HostLocal:
			if p.BlockScopeLocal() {
				conn.Block("Localhost access blocked") // Block Outbound / Drop Inbound
				return true
			}
		default: // netutils.Invalid
			conn.Deny("invalid IP") // Block Outbound / Drop Inbound
			return true
		}
	} else if conn.Entity.Domain != "" {
		// DNS Query
		// DNS is expected to resolve to LAN or Internet addresses
		// TODO: handle domains mapped to localhost
		if p.BlockScopeInternet() && p.BlockScopeLAN() {
			conn.Block("Internet and LAN access blocked")
			return true
		}
	}
	return false
}

func checkBypassPrevention(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	if conn.Process().Profile().PreventBypassing() {
		// check for bypass protection
		result, reason, reasonCtx := PreventBypassing(conn)
		switch result {
		case endpoints.Denied:
			conn.BlockWithContext("bypass prevention: "+reason, reasonCtx)
			return true
		case endpoints.Permitted:
			conn.AcceptWithContext("bypass prevention: "+reason, reasonCtx)
			return true
		case endpoints.NoMatch:
		}
	}
	return false
}

func checkFilterLists(ctx context.Context, conn *network.Connection, pkt packet.Packet) bool {
	// apply privacy filter lists
	p := conn.Process().Profile()

	result, reason := p.MatchFilterLists(conn.Entity)
	switch result {
	case endpoints.Denied:
		conn.DenyWithContext(reason.String(), reason.Context())
		return true
	case endpoints.NoMatch:
		// nothing to do
	default:
		log.Tracer(ctx).Debugf("filter: filter lists returned unsupported verdict: %s", result)
	}
	return false
}

func checkInbound(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	// implicit default=block for inbound
	if conn.Inbound {
		conn.Drop("endpoint is not whitelisted (incoming is always default=block)")
		return true
	}
	return false
}

func checkDefaultPermit(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	// check default action
	p := conn.Process().Profile()
	if p.DefaultAction() == profile.DefaultActionPermit {
		conn.Accept("endpoint is not blacklisted (default=permit)")
		return true
	}
	return false
}

func checkAutoPermitRelated(_ context.Context, conn *network.Connection, _ packet.Packet) bool {
	p := conn.Process().Profile()
	if !p.DisableAutoPermit() {
		related, reason := checkRelation(conn)
		if related {
			conn.Accept(reason)
			return true
		}
	}
	return false
}

func checkDefaultAction(_ context.Context, conn *network.Connection, pkt packet.Packet) bool {
	p := conn.Process().Profile()
	if p.DefaultAction() == profile.DefaultActionAsk {
		prompt(conn, pkt)
		return true
	}
	return false
}

// checkRelation tries to find a relation between a process and a communication. This is for better out of the box experience and is _not_ meant to thwart intentional malware.
func checkRelation(conn *network.Connection) (related bool, reason string) {
	if conn.Entity.Domain != "" {
		return false, ""
	}
	// don't check for unknown processes
	if conn.Process().Pid < 0 {
		return false, ""
	}

	pathElements := strings.Split(conn.Process().Path, string(filepath.Separator))
	// only look at the last two path segments
	if len(pathElements) > 2 {
		pathElements = pathElements[len(pathElements)-2:]
	}
	domainElements := strings.Split(conn.Entity.Domain, ".")

	var domainElement string
	var processElement string

matchLoop:
	for _, domainElement = range domainElements {
		for _, pathElement := range pathElements {
			if levenshtein.Match(domainElement, pathElement, nil) > 0.5 {
				related = true
				processElement = pathElement
				break matchLoop
			}
		}
		if levenshtein.Match(domainElement, conn.Process().Name, nil) > 0.5 {
			related = true
			processElement = conn.Process().Name
			break matchLoop
		}
		if levenshtein.Match(domainElement, conn.Process().ExecName, nil) > 0.5 {
			related = true
			processElement = conn.Process().ExecName
			break matchLoop
		}
	}

	if related {
		reason = fmt.Sprintf("domain is related to process: %s is related to %s", domainElement, processElement)
	}
	return related, reason
}
