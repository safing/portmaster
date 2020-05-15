package firewall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
func DecideOnConnection(conn *network.Connection, pkt packet.Packet) {
	// update profiles and check if communication needs reevaluation
	if conn.UpdateAndCheck() {
		log.Infof("filter: re-evaluating verdict on %s", conn)
		conn.Verdict = network.VerdictUndecided

		if conn.Entity != nil {
			conn.Entity.ResetLists()
		}
	}

	var deciders = []func(*network.Connection, packet.Packet) bool{
		checkPortmasterConnection,
		checkSelfCommunication,
		checkProfileExists,
		checkConnectionType,
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
		if decider(conn, pkt) {
			return
		}
	}

	// DefaultAction == DefaultActionBlock
	conn.Deny("endpoint is not whitelisted (default=block)")
}

// checkPortmasterConnection allows all connection that originate from
// portmaster itself.
func checkPortmasterConnection(conn *network.Connection, _ packet.Packet) bool {
	// grant self
	if conn.Process().Pid == os.Getpid() {
		log.Infof("filter: granting own connection %s", conn)
		conn.Verdict = network.VerdictAccept
		conn.Internal = true
		return true
	}

	return false
}

// checkSelfCommunication checks if the process is communicating with itself.
func checkSelfCommunication(conn *network.Connection, pkt packet.Packet) bool {
	// check if process is communicating with itself
	if pkt != nil {
		// TODO: evaluate the case where different IPs in the 127/8 net are used.
		pktInfo := pkt.Info()
		if conn.Process().Pid >= 0 && pktInfo.Src.Equal(pktInfo.Dst) {
			// get PID
			otherPid, _, err := state.Lookup(
				pktInfo.Version,
				pktInfo.Protocol,
				pktInfo.RemoteIP(),
				pktInfo.RemotePort(),
				pktInfo.LocalIP(),
				pktInfo.LocalPort(),
				pktInfo.Direction,
			)
			if err != nil {
				log.Warningf("filter: failed to find local peer process PID: %s", err)
			} else {
				// get primary process
				otherProcess, err := process.GetOrFindPrimaryProcess(pkt.Ctx(), otherPid)
				if err != nil {
					log.Warningf("filter: failed to find load local peer process with PID %d: %s", otherPid, err)
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

func checkProfileExists(conn *network.Connection, _ packet.Packet) bool {
	if conn.Process().Profile() == nil {
		conn.Block("unknown process or profile")
		return true
	}
	return false
}

func checkEndpointLists(conn *network.Connection, _ packet.Packet) bool {
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

func checkConnectionType(conn *network.Connection, _ packet.Packet) bool {
	p := conn.Process().Profile()

	// check conn type
	switch conn.Scope {
	case network.IncomingHost, network.IncomingLAN, network.IncomingInternet, network.IncomingInvalid:
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

func checkConnectionScope(conn *network.Connection, _ packet.Packet) bool {
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

func checkBypassPrevention(conn *network.Connection, _ packet.Packet) bool {
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

func checkFilterLists(conn *network.Connection, _ packet.Packet) bool {
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
		log.Debugf("filter: filter lists returned unsupported verdict: %s", result)
	}
	return false
}

func checkInbound(conn *network.Connection, _ packet.Packet) bool {
	// implicit default=block for inbound
	if conn.Inbound {
		conn.Drop("endpoint is not whitelisted (incoming is always default=block)")
		return true
	}
	return false
}

func checkDefaultPermit(conn *network.Connection, _ packet.Packet) bool {
	// check default action
	p := conn.Process().Profile()
	if p.DefaultAction() == profile.DefaultActionPermit {
		conn.Accept("endpoint is not blacklisted (default=permit)")
		return true
	}
	return false
}

func checkAutoPermitRelated(conn *network.Connection, _ packet.Packet) bool {
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

func checkDefaultAction(conn *network.Connection, pkt packet.Packet) bool {
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
