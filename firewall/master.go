package firewall

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/resolver"

	"github.com/agext/levenshtein"
	"github.com/miekg/dns"
)

// Call order:
//
// DNS Query:
// 1. DecideOnConnection
//    is called when a DNS query is made, may set verdict to Undeterminable to permit a DNS reply.
//    is called with a nil packet.
// 2. FilterDNSResponse
//    is called to (possibly) filter out A/AAAA records that the filter would deny later.
//
// Network Connection:
// 3. DecideOnConnection
//    is called with the first packet of a network connection.

// DecideOnConnection makes a decision about a connection.
// When called, the connection and profile is already locked.
func DecideOnConnection(conn *network.Connection, pkt packet.Packet) { //nolint:gocognit,gocyclo // TODO
	// update profiles and check if communication needs reevaluation
	if conn.UpdateAndCheck() {
		log.Infof("filter: re-evaluating verdict on %s", conn)
		conn.Verdict = network.VerdictUndecided

		if conn.Entity != nil {
			conn.Entity.ResetLists()
		}
	}

	// grant self
	if conn.Process().Pid == os.Getpid() {
		log.Infof("filter: granting own connection %s", conn)
		conn.Verdict = network.VerdictAccept
		conn.Internal = true
		return
	}

	// check if process is communicating with itself
	if pkt != nil {
		// TODO: evaluate the case where different IPs in the 127/8 net are used.
		pktInfo := pkt.Info()
		if conn.Process().Pid >= 0 && pktInfo.Src.Equal(pktInfo.Dst) {
			// get PID
			otherPid, _, err := process.GetPidByEndpoints(
				pktInfo.RemoteIP(),
				pktInfo.RemotePort(),
				pktInfo.LocalIP(),
				pktInfo.LocalPort(),
				pktInfo.Protocol,
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
					return
				}
			}
		}
	}

	// get profile
	p := conn.Process().Profile()
	if p == nil {
		conn.Block("no profile")
		return
	}

	// check conn type
	switch conn.Scope {
	case network.IncomingHost, network.IncomingLAN, network.IncomingInternet, network.IncomingInvalid:
		if p.BlockInbound() {
			if conn.Scope == network.IncomingHost {
				conn.Block("inbound connections blocked")
			} else {
				conn.Drop("inbound connections blocked")
			}
			return
		}
	case network.PeerLAN, network.PeerInternet, network.PeerInvalid:
		// Important: PeerHost is and should be missing!
		if p.BlockP2P() {
			conn.Block("direct connections (P2P) blocked")
			return
		}
	}

	// check scopes
	if conn.Entity.IP != nil {
		classification := netutils.ClassifyIP(conn.Entity.IP)

		switch classification {
		case netutils.Global, netutils.GlobalMulticast:
			if p.BlockScopeInternet() {
				conn.Deny("Internet access blocked") // Block Outbound / Drop Inbound
				return
			}
		case netutils.SiteLocal, netutils.LinkLocal, netutils.LocalMulticast:
			if p.BlockScopeLAN() {
				conn.Block("LAN access blocked") // Block Outbound / Drop Inbound
				return
			}
		case netutils.HostLocal:
			if p.BlockScopeLocal() {
				conn.Block("Localhost access blocked") // Block Outbound / Drop Inbound
				return
			}
		default: // netutils.Invalid
			conn.Deny("invalid IP") // Block Outbound / Drop Inbound
			return
		}
	} else if conn.Entity.Domain != "" {
		// DNS Query
		// DNS is expected to resolve to LAN or Internet addresses
		// TODO: handle domains mapped to localhost
		if p.BlockScopeInternet() && p.BlockScopeLAN() {
			conn.Block("Internet and LAN access blocked")
			return
		}
	}

	// check endpoints list
	var result endpoints.EPResult
	var reason string
	if conn.Inbound {
		result, reason = p.MatchServiceEndpoint(conn.Entity)
	} else {
		result, reason = p.MatchEndpoint(conn.Entity)
	}
	switch result {
	case endpoints.Denied:
		conn.Deny("endpoint is blacklisted: " + reason) // Block Outbound / Drop Inbound
		return
	case endpoints.Permitted:
		conn.Accept("endpoint is whitelisted: " + reason)
		return
	}
	// continuing with result == NoMatch

	// apply privacy filter lists
	result, reason = p.MatchFilterLists(conn.Entity)
	switch result {
	case endpoints.Denied:
		conn.Deny("endpoint in filterlists: " + reason)
		return
	case endpoints.NoMatch:
		// nothing to do
	default:
		log.Debugf("filter: filter lists returned unsupported verdict: %s", result)
	}

	// implicit default=block for inbound
	if conn.Inbound {
		conn.Drop("endpoint is not whitelisted (incoming is always default=block)")
		return
	}

	// check default action
	if p.DefaultAction() == profile.DefaultActionPermit {
		conn.Accept("endpoint is not blacklisted (default=permit)")
		return
	}

	// check relation
	if !p.DisableAutoPermit() {
		related, reason := checkRelation(conn)
		if related {
			conn.Accept(reason)
			return
		}
	}

	// prompt
	if p.DefaultAction() == profile.DefaultActionAsk {
		prompt(conn, pkt)
		return
	}

	// DefaultAction == DefaultActionBlock
	conn.Deny("endpoint is not whitelisted (default=block)")
}

// FilterDNSResponse filters a dns response according to the application profile and settings.
func FilterDNSResponse(conn *network.Connection, q *resolver.Query, rrCache *resolver.RRCache) *resolver.RRCache { //nolint:gocognit // TODO
	// do not modify own queries
	if conn.Process().Pid == os.Getpid() {
		return rrCache
	}

	// get profile
	p := conn.Process().Profile()
	if p == nil {
		conn.Block("no profile")
		return nil
	}

	// check if DNS response filtering is completely turned off
	if !p.RemoveOutOfScopeDNS() && !p.RemoveBlockedDNS() {
		return rrCache
	}

	// duplicate entry
	rrCache = rrCache.ShallowCopy()
	rrCache.FilteredEntries = make([]string, 0)

	// change information
	var addressesRemoved int
	var addressesOk int

	// loop vars
	var classification int8
	var ip net.IP

	// filter function
	filterEntries := func(entries []dns.RR) (goodEntries []dns.RR) {
		goodEntries = make([]dns.RR, 0, len(entries))

		for _, rr := range entries {

			// get IP and classification
			switch v := rr.(type) {
			case *dns.A:
				ip = v.A
			case *dns.AAAA:
				ip = v.AAAA
			default:
				// add non A/AAAA entries
				goodEntries = append(goodEntries, rr)
				continue
			}
			classification = netutils.ClassifyIP(ip)

			if p.RemoveOutOfScopeDNS() {
				switch {
				case classification == netutils.HostLocal:
					// No DNS should return localhost addresses
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				case rrCache.ServerScope == netutils.Global && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
					// No global DNS should return LAN addresses
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				}
			}

			if p.RemoveBlockedDNS() {
				// filter by flags
				switch {
				case p.BlockScopeInternet() && classification == netutils.Global:
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				case p.BlockScopeLAN() && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				case p.BlockScopeLocal() && classification == netutils.HostLocal:
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				}

				// TODO: filter by endpoint list (IP only)
			}

			// if survived, add to good entries
			addressesOk++
			goodEntries = append(goodEntries, rr)
		}
		return
	}

	rrCache.Answer = filterEntries(rrCache.Answer)
	rrCache.Extra = filterEntries(rrCache.Extra)

	if addressesRemoved > 0 {
		rrCache.Filtered = true
		if addressesOk == 0 {
			conn.Block("no addresses returned for this domain are permitted")
			return nil
		}
	}

	if rrCache.Filtered {
		log.Infof("filter: filtered DNS replies for %s: %s", conn, strings.Join(rrCache.FilteredEntries, ", "))
	}

	// TODO: Gate17 integration
	// tunnelInfo, err := AssignTunnelIP(fqdn)

	return rrCache
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
