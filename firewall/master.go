package firewall

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/intel"
	"github.com/Safing/portmaster/network"
	"github.com/Safing/portmaster/network/netutils"
	"github.com/Safing/portmaster/network/packet"
	"github.com/Safing/portmaster/profile"
	"github.com/Safing/portmaster/status"
	"github.com/miekg/dns"

	"github.com/agext/levenshtein"
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

// DecideOnCommunicationBeforeIntel makes a decision about a communication before the dns query is resolved and intel is gathered.
func DecideOnCommunicationBeforeIntel(comm *network.Communication, fqdn string) {

	// grant self
	if comm.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own communication %s", comm)
		comm.Accept("")
		return
	}

	// get and check profile set
	profileSet := comm.Process().ProfileSet()
	if profileSet == nil {
		log.Errorf("firewall: denying communication %s, no Profile Set", comm)
		comm.Deny("no Profile Set")
		return
	}
	profileSet.Update(status.ActiveSecurityLevel())

	// check for any network access
	if !profileSet.CheckFlag(profile.Internet) && !profileSet.CheckFlag(profile.LAN) {
		log.Infof("firewall: denying communication %s, accessing Internet or LAN not permitted", comm)
		comm.Deny("accessing Internet or LAN not permitted")
		return
	}

	// check endpoint list
	result, reason := profileSet.CheckEndpointDomain(fqdn)
	switch result {
	case profile.NoMatch:
		comm.UpdateVerdict(network.VerdictUndecided)
	case profile.Undeterminable:
		comm.UpdateVerdict(network.VerdictUndeterminable)
		return
	case profile.Denied:
		log.Infof("firewall: denying communication %s, endpoint is blacklisted: %s", comm, reason)
		comm.Deny(fmt.Sprintf("endpoint is blacklisted: %s", reason))
		return
	case profile.Permitted:
		log.Infof("firewall: permitting communication %s, endpoint is whitelisted: %s", comm, reason)
		comm.Accept(fmt.Sprintf("endpoint is whitelisted: %s", reason))
		return
	}

	switch profileSet.GetProfileMode() {
	case profile.Whitelist:
		log.Infof("firewall: denying communication %s, domain is not whitelisted", comm)
		comm.Deny("domain is not whitelisted")
		return
	case profile.Prompt:

		// check Related flag
		// TODO: improve this!
		if profileSet.CheckFlag(profile.Related) {
			matched := false
			pathElements := strings.Split(comm.Process().Path, "/") // FIXME: path seperator
			// only look at the last two path segments
			if len(pathElements) > 2 {
				pathElements = pathElements[len(pathElements)-2:]
			}
			domainElements := strings.Split(fqdn, ".")

			var domainElement string
			var processElement string

		matchLoop:
			for _, domainElement = range domainElements {
				for _, pathElement := range pathElements {
					if levenshtein.Match(domainElement, pathElement, nil) > 0.5 {
						matched = true
						processElement = pathElement
						break matchLoop
					}
				}
				if levenshtein.Match(domainElement, profileSet.UserProfile().Name, nil) > 0.5 {
					matched = true
					processElement = profileSet.UserProfile().Name
					break matchLoop
				}
				if levenshtein.Match(domainElement, comm.Process().Name, nil) > 0.5 {
					matched = true
					processElement = comm.Process().Name
					break matchLoop
				}
				if levenshtein.Match(domainElement, comm.Process().ExecName, nil) > 0.5 {
					matched = true
					processElement = comm.Process().ExecName
					break matchLoop
				}
			}

			if matched {
				log.Infof("firewall: permitting communication %s, match to domain was found: %s ~== %s", comm, domainElement, processElement)
				comm.Accept("domain is related to process")
			}
		}

		if comm.GetVerdict() != network.VerdictAccept {
			// TODO
			log.Infof("firewall: permitting communication %s, domain permitted (prompting is not yet implemented)", comm)
			comm.Accept("domain permitted (prompting is not yet implemented)")
		}
		return
	case profile.Blacklist:
		log.Infof("firewall: permitting communication %s, domain is not blacklisted", comm)
		comm.Accept("domain is not blacklisted")
		return
	}

	log.Infof("firewall: denying communication %s, no profile mode set", comm)
	comm.Deny("no profile mode set")
}

// DecideOnCommunicationAfterIntel makes a decision about a communication after the dns query is resolved and intel is gathered.
func DecideOnCommunicationAfterIntel(comm *network.Communication, fqdn string, rrCache *intel.RRCache) {

	// SUSPENDED until Stamp integration is finished

	// grant self - should not get here
	// if comm.Process().Pid == os.Getpid() {
	// 	log.Infof("firewall: granting own communication %s", comm)
	// 	comm.Accept("")
	// 	return
	// }

	// check if there is a profile
	// profileSet := comm.Process().ProfileSet()
	// if profileSet == nil {
	// 	log.Errorf("firewall: denying communication %s, no Profile Set", comm)
	// 	comm.Deny("no Profile Set")
	// 	return
	// }
	// profileSet.Update(status.ActiveSecurityLevel())

	// TODO: Stamp integration

	return
}

// FilterDNSResponse filters a dns response according to the application profile and settings.
func FilterDNSResponse(comm *network.Communication, fqdn string, rrCache *intel.RRCache) *intel.RRCache {
	// do not modify own queries - this should not happen anyway
	if comm.Process().Pid == os.Getpid() {
		return rrCache
	}

	// check if there is a profile
	profileSet := comm.Process().ProfileSet()
	if profileSet == nil {
		log.Infof("firewall: blocking dns query of communication %s, no Profile Set", comm)
		return nil
	}
	profileSet.Update(status.ActiveSecurityLevel())

	// save config for consistency during function call
	secLevel := profileSet.SecurityLevel()
	filterByScope := filterDNSByScope(secLevel)
	filterByProfile := filterDNSByProfile(secLevel)

	// check if DNS response filtering is completely turned off
	if !filterByScope && !filterByProfile {
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
	var result profile.EPResult

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

			if filterByScope {
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

			if filterByProfile {
				// filter by flags
				switch {
				case !profileSet.CheckFlag(profile.Internet) && classification == netutils.Global:
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				case !profileSet.CheckFlag(profile.LAN) && (classification == netutils.SiteLocal || classification == netutils.LinkLocal):
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				case !profileSet.CheckFlag(profile.Localhost) && classification == netutils.HostLocal:
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				}

				// filter by endpoints
				result, _ = profileSet.CheckEndpointIP("", ip, 0, 0, false)
				if result == profile.Denied {
					addressesRemoved++
					rrCache.FilteredEntries = append(rrCache.FilteredEntries, rr.String())
					continue
				}
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
func DecideOnCommunication(comm *network.Communication, pkt packet.Packet) {

	// grant self
	if comm.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own communication %s", comm)
		comm.Accept("")
		return
	}

	// check if there is a profile
	profileSet := comm.Process().ProfileSet()
	if profileSet == nil {
		log.Errorf("firewall: denying communication %s, no Profile Set", comm)
		comm.Deny("no Profile Set")
		return
	}
	profileSet.Update(status.ActiveSecurityLevel())

	// check comm type
	switch comm.Domain {
	case network.IncomingHost, network.IncomingLAN, network.IncomingInternet, network.IncomingInvalid:
		if !profileSet.CheckFlag(profile.Service) {
			log.Infof("firewall: denying communication %s, not a service", comm)
			if comm.Domain == network.IncomingHost {
				comm.Block("not a service")
			} else {
				comm.Drop("not a service")
			}
			return
		}
	case network.PeerLAN, network.PeerInternet, network.PeerInvalid: // Important: PeerHost is and should be missing!
		if !profileSet.CheckFlag(profile.PeerToPeer) {
			log.Infof("firewall: denying communication %s, peer to peer comms (to an IP) not allowed", comm)
			comm.Deny("peer to peer comms (to an IP) not allowed")
			return
		}
	}

	// check network scope
	switch comm.Domain {
	case network.IncomingHost:
		if !profileSet.CheckFlag(profile.Localhost) {
			log.Infof("firewall: denying communication %s, serving localhost not allowed", comm)
			comm.Block("serving localhost not allowed")
			return
		}
	case network.IncomingLAN:
		if !profileSet.CheckFlag(profile.LAN) {
			log.Infof("firewall: denying communication %s, serving LAN not allowed", comm)
			comm.Deny("serving LAN not allowed")
			return
		}
	case network.IncomingInternet:
		if !profileSet.CheckFlag(profile.Internet) {
			log.Infof("firewall: denying communication %s, serving Internet not allowed", comm)
			comm.Deny("serving Internet not allowed")
			return
		}
	case network.IncomingInvalid:
		log.Infof("firewall: denying communication %s, invalid IP address", comm)
		comm.Drop("invalid IP address")
		return
	case network.PeerHost:
		if !profileSet.CheckFlag(profile.Localhost) {
			log.Infof("firewall: denying communication %s, accessing localhost not allowed", comm)
			comm.Block("accessing localhost not allowed")
			return
		}
	case network.PeerLAN:
		if !profileSet.CheckFlag(profile.LAN) {
			log.Infof("firewall: denying communication %s, accessing the LAN not allowed", comm)
			comm.Deny("accessing the LAN not allowed")
			return
		}
	case network.PeerInternet:
		if !profileSet.CheckFlag(profile.Internet) {
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
}

// DecideOnLink makes a decision about a link with the first packet.
func DecideOnLink(comm *network.Communication, link *network.Link, pkt packet.Packet) {
	// check:
	// Profile.Flags
	// - network specific: Internet, LocalNet
	// Profile.ConnectPorts
	// Profile.ListenPorts

	// grant self
	if comm.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own link %s", comm)
		link.Accept("")
		return
	}

	// check if there is a profile
	profileSet := comm.Process().ProfileSet()
	if profileSet == nil {
		log.Infof("firewall: no Profile Set, denying %s", link)
		link.Deny("no Profile Set")
		return
	}
	profileSet.Update(status.ActiveSecurityLevel())

	// get domain
	var domain string
	if strings.HasSuffix(comm.Domain, ".") {
		domain = comm.Domain
	}

	// remoteIP
	var remoteIP net.IP
	if comm.Direction {
		remoteIP = pkt.GetIPHeader().Src
	} else {
		remoteIP = pkt.GetIPHeader().Dst
	}

	// protocol and destination port
	protocol := uint8(pkt.GetIPHeader().Protocol)
	var dstPort uint16
	tcpUDPHeader := pkt.GetTCPUDPHeader()
	if tcpUDPHeader != nil {
		dstPort = tcpUDPHeader.DstPort
	}

	// check endpoints list
	result, reason := profileSet.CheckEndpointIP(domain, remoteIP, protocol, dstPort, comm.Direction)
	switch result {
	// case profile.NoMatch, profile.Undeterminable:
	// 	continue
	case profile.Denied:
		log.Infof("firewall: denying link %s, endpoint is blacklisted: %s", link, reason)
		link.Deny(fmt.Sprintf("endpoint is blacklisted: %s", reason))
		return
	case profile.Permitted:
		log.Infof("firewall: permitting link %s, endpoint is whitelisted: %s", link, reason)
		link.Accept(fmt.Sprintf("endpoint is whitelisted: %s", reason))
		return
	}

	switch profileSet.GetProfileMode() {
	case profile.Whitelist:
		log.Infof("firewall: denying link %s: endpoint is not whitelisted", link)
		link.Deny("endpoint is not whitelisted")
		return
	case profile.Prompt:
		log.Infof("firewall: permitting link %s: endpoint is not blacklisted (prompting is not yet implemented)", link)
		link.Accept("endpoint is not blacklisted (prompting is not yet implemented)")
		return
	case profile.Blacklist:
		log.Infof("firewall: permitting link %s: endpoint is not blacklisted", link)
		link.Accept("endpoint is not blacklisted")
		return
	}

	log.Infof("firewall: denying link %s, no profile mode set", link)
	link.Deny("no profile mode set")
}
