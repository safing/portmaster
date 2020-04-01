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
// 1. DecideOnCommunicationBeforeIntel (if connecting to domain)
//    is called when a DNS query is made, before the query is resolved
// 2. DecideOnCommunicationAfterIntel (if connecting to domain)
//    is called when a DNS query is made, after the query is resolved
// 3. DecideOnCommunication
//    is called when the first packet of the first link of the communication arrives
// 4. DecideOnLink
//		is called when when the first packet of a link arrives only if communication has verdict UNDECIDED or CANTSAY

// DecideOnCommunicationBeforeDNS makes a decision about a communication before the dns query is resolved and intel is gathered.
func DecideOnCommunicationBeforeDNS(comm *network.Communication) {
	// update profiles and check if communication needs reevaluation
	if comm.UpdateAndCheck() {
		log.Infof("firewall: re-evaluating verdict on %s", comm)
		comm.ResetVerdict()
	}

	// check if need to run
	if comm.GetVerdict() != network.VerdictUndecided {
		return
	}

	// grant self
	if comm.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own communication %s", comm)
		comm.Accept("")
		return
	}

	// get profile
	p := comm.Process().Profile()

	// check for any network access
	if p.BlockScopeInternet() && p.BlockScopeLAN() {
		log.Infof("firewall: denying communication %s, accessing Internet or LAN not permitted", comm)
		comm.Deny("accessing Internet or LAN not permitted")
		return
	}
	// continueing with access to either Internet or LAN

	// check endpoint list
	// FIXME: comm.Entity.Lock()
	result, reason := p.MatchEndpoint(comm.Entity)
	// FIXME: comm.Entity.Unlock()
	switch result {
	case endpoints.Undeterminable:
		comm.UpdateVerdict(network.VerdictUndeterminable)
		return
	case endpoints.Denied:
		log.Infof("firewall: denying communication %s, domain is blacklisted: %s", comm, reason)
		comm.Deny(fmt.Sprintf("domain is blacklisted: %s", reason))
		return
	case endpoints.Permitted:
		log.Infof("firewall: permitting communication %s, domain is whitelisted: %s", comm, reason)
		comm.Accept(fmt.Sprintf("domain is whitelisted: %s", reason))
		return
	}
	// continueing with result == NoMatch

	// check default action
	if p.DefaultAction() == profile.DefaultActionPermit {
		log.Infof("firewall: permitting communication %s, domain is not blacklisted (default=permit)", comm)
		comm.Accept("domain is not blacklisted (default=permit)")
		return
	}

	// check relation
	if !p.DisableAutoPermit() {
		if checkRelation(comm) {
			return
		}
	}

	// prompt
	if p.DefaultAction() == profile.DefaultActionAsk {
		prompt(comm, nil, nil)
		return
	}

	// DefaultAction == DefaultActionBlock
	log.Infof("firewall: denying communication %s, domain is not whitelisted (default=block)", comm)
	comm.Deny("domain is not whitelisted (default=block)")
	return
}

// FilterDNSResponse filters a dns response according to the application profile and settings.
func FilterDNSResponse(comm *network.Communication, q *resolver.Query, rrCache *resolver.RRCache) *resolver.RRCache { //nolint:gocognit // TODO
	// do not modify own queries - this should not happen anyway
	if comm.Process().Pid == os.Getpid() {
		return rrCache
	}

	// get profile
	p := comm.Process().Profile()

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
			comm.Deny("no addresses returned for this domain are permitted")
			log.Infof("firewall: fully dns responses for communication %s", comm)
			return nil
		}
	}

	if rrCache.Filtered {
		log.Infof("firewall: filtered DNS replies for %s: %s", comm, strings.Join(rrCache.FilteredEntries, ", "))
	}

	// TODO: Gate17 integration
	// tunnelInfo, err := AssignTunnelIP(fqdn)

	return rrCache
}

// DecideOnCommunication makes a decision about a communication with its first packet.
func DecideOnCommunication(comm *network.Communication) {
	// update profiles and check if communication needs reevaluation
	if comm.UpdateAndCheck() {
		log.Infof("firewall: re-evaluating verdict on %s", comm)
		comm.ResetVerdict()

		// if communicating with a domain entity, re-evaluate with BeforeDNS
		if strings.HasSuffix(comm.Scope, ".") {
			DecideOnCommunicationBeforeDNS(comm)
		}
	}

	// check if need to run
	if comm.GetVerdict() != network.VerdictUndecided {
		return
	}

	// grant self
	if comm.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own communication %s", comm)
		comm.Accept("")
		return
	}

	// get profile
	p := comm.Process().Profile()

	// check comm type
	switch comm.Scope {
	case network.IncomingHost, network.IncomingLAN, network.IncomingInternet, network.IncomingInvalid:
		if p.BlockInbound() {
			log.Infof("firewall: denying communication %s, not a service", comm)
			if comm.Scope == network.IncomingHost {
				comm.Block("not a service")
			} else {
				comm.Deny("not a service")
			}
			return
		}
	case network.PeerLAN, network.PeerInternet, network.PeerInvalid:
		// Important: PeerHost is and should be missing!
		if p.BlockP2P() {
			log.Infof("firewall: denying communication %s, peer to peer comms (to an IP) not allowed", comm)
			comm.Deny("peer to peer comms (to an IP) not allowed")
			return
		}
	}

	// check network scope
	switch comm.Scope {
	case network.IncomingHost:
		if p.BlockScopeLocal() {
			log.Infof("firewall: denying communication %s, serving localhost not allowed", comm)
			comm.Block("serving localhost not allowed")
			return
		}
	case network.IncomingLAN:
		if p.BlockScopeLAN() {
			log.Infof("firewall: denying communication %s, serving LAN not allowed", comm)
			comm.Deny("serving LAN not allowed")
			return
		}
	case network.IncomingInternet:
		if p.BlockScopeInternet() {
			log.Infof("firewall: denying communication %s, serving Internet not allowed", comm)
			comm.Deny("serving Internet not allowed")
			return
		}
	case network.IncomingInvalid:
		log.Infof("firewall: denying communication %s, invalid IP address", comm)
		comm.Drop("invalid IP address")
		return
	case network.PeerHost:
		if p.BlockScopeLocal() {
			log.Infof("firewall: denying communication %s, accessing localhost not allowed", comm)
			comm.Block("accessing localhost not allowed")
			return
		}
	case network.PeerLAN:
		if p.BlockScopeLAN() {
			log.Infof("firewall: denying communication %s, accessing the LAN not allowed", comm)
			comm.Deny("accessing the LAN not allowed")
			return
		}
	case network.PeerInternet:
		if p.BlockScopeInternet() {
			log.Infof("firewall: denying communication %s, accessing the Internet not allowed", comm)
			comm.Deny("accessing the Internet not allowed")
			return
		}
	case network.PeerInvalid:
		log.Infof("firewall: denying communication %s, invalid IP address", comm)
		comm.Deny("invalid IP address")
		return
	}

	log.Infof("firewall: undeterminable verdict for communication %s", comm)
	comm.UpdateVerdict(network.VerdictUndeterminable)
}

// DecideOnLink makes a decision about a link with the first packet.
func DecideOnLink(comm *network.Communication, link *network.Link, pkt packet.Packet) {

	// grant self
	if comm.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own link %s", comm)
		link.Accept("")
		return
	}

	// check if process is communicating with itself
	if comm.Process().Pid >= 0 && pkt.Info().Src.Equal(pkt.Info().Dst) {
		// get PID
		otherPid, _, err := process.GetPidByEndpoints(
			pkt.Info().RemoteIP(),
			pkt.Info().RemotePort(),
			pkt.Info().LocalIP(),
			pkt.Info().LocalPort(),
			pkt.Info().Protocol,
		)
		if err == nil {

			// get primary process
			otherProcess, err := process.GetOrFindPrimaryProcess(pkt.Ctx(), otherPid)
			if err == nil {

				if otherProcess.Pid == comm.Process().Pid {
					log.Infof("firewall: permitting connection to self %s", comm)
					link.AddReason("connection to self")

					link.Lock()
					link.Verdict = network.VerdictAccept
					link.SaveWhenFinished()
					link.Unlock()
					return
				}

			}
		}
	}

	// check if we aleady have a verdict
	switch comm.GetVerdict() {
	case network.VerdictUndecided, network.VerdictUndeterminable:
		// continue
	default:
		link.UpdateVerdict(comm.GetVerdict())
		return
	}

	// get profile
	p := comm.Process().Profile()

	// check endpoints list
	var result endpoints.EPResult
	var reason string
	// FIXME: link.Entity.Lock()
	if comm.Direction {
		result, reason = p.MatchServiceEndpoint(link.Entity)
	} else {
		result, reason = p.MatchEndpoint(link.Entity)
	}
	// FIXME: link.Entity.Unlock()
	switch result {
	case endpoints.Denied:
		log.Infof("firewall: denying link %s, endpoint is blacklisted: %s", link, reason)
		link.Deny(fmt.Sprintf("endpoint is blacklisted: %s", reason))
		return
	case endpoints.Permitted:
		log.Infof("firewall: permitting link %s, endpoint is whitelisted: %s", link, reason)
		link.Accept(fmt.Sprintf("endpoint is whitelisted: %s", reason))
		return
	}
	// continueing with result == NoMatch

	// implicit default=block for incoming
	if comm.Direction {
		log.Infof("firewall: denying link %s: endpoint is not whitelisted (incoming is always default=block)", link)
		link.Deny("endpoint is not whitelisted (incoming is always default=block)")
		return
	}

	// check default action
	if p.DefaultAction() == profile.DefaultActionPermit {
		log.Infof("firewall: permitting link %s: endpoint is not blacklisted (default=permit)", link)
		link.Accept("endpoint is not blacklisted (default=permit)")
		return
	}

	// check relation
	if !p.DisableAutoPermit() {
		if checkRelation(comm) {
			return
		}
	}

	// prompt
	if p.DefaultAction() == profile.DefaultActionAsk {
		prompt(comm, link, pkt)
		return
	}

	// DefaultAction == DefaultActionBlock
	log.Infof("firewall: denying link %s: endpoint is not whitelisted (default=block)", link)
	link.Deny("endpoint is not whitelisted (default=block)")
	return
}

// checkRelation tries to find a relation between a process and a communication. This is for better out of the box experience and is _not_ meant to thwart intentional malware.
func checkRelation(comm *network.Communication) (related bool) {
	if comm.Entity.Domain != "" {
		return false
	}
	// don't check for unknown processes
	if comm.Process().Pid < 0 {
		return false
	}

	pathElements := strings.Split(comm.Process().Path, string(filepath.Separator))
	// only look at the last two path segments
	if len(pathElements) > 2 {
		pathElements = pathElements[len(pathElements)-2:]
	}
	domainElements := strings.Split(comm.Entity.Domain, ".")

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
		if levenshtein.Match(domainElement, comm.Process().Name, nil) > 0.5 {
			related = true
			processElement = comm.Process().Name
			break matchLoop
		}
		if levenshtein.Match(domainElement, comm.Process().ExecName, nil) > 0.5 {
			related = true
			processElement = comm.Process().ExecName
			break matchLoop
		}
	}

	if related {
		log.Infof("firewall: permitting communication %s, match to domain was found: %s is related to %s", comm, domainElement, processElement)
		comm.Accept(fmt.Sprintf("domain is related to process: %s is related to %s", domainElement, processElement))
	}
	return related
}
