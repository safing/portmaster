package firewall

import (
	"os"
	"strings"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/intel"
	"github.com/Safing/portmaster/network"
	"github.com/Safing/portmaster/network/packet"
	"github.com/Safing/portmaster/profile"
	"github.com/Safing/portmaster/status"

	"github.com/agext/levenshtein"
)

// Call order:
//
// 1. DecideOnConnectionBeforeIntel (if connecting to domain)
//    is called when a DNS query is made, before the query is resolved
// 2. DecideOnConnectionAfterIntel (if connecting to domain)
//    is called when a DNS query is made, after the query is resolved
// 3. DecideOnConnection
//    is called when the first packet of the first link of the connection arrives
// 4. DecideOnLink
//		is called when when the first packet of a link arrives only if connection has verdict UNDECIDED or CANTSAY

// DecideOnConnectionBeforeIntel makes a decision about a connection before the dns query is resolved and intel is gathered.
func DecideOnConnectionBeforeIntel(connection *network.Connection, fqdn string) {
	// check:
	// Profile.DomainWhitelist
	// Profile.Flags
	// - process specific: System, Admin, User
	// - network specific: Internet, LocalNet

	// grant self
	if connection.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own connection %s", connection)
		connection.Accept("")
		return
	}

	// check if there is a profile
	profileSet := connection.Process().ProfileSet()
	if profileSet == nil {
		log.Errorf("firewall: denying connection %s, no profile set", connection)
		connection.Deny("no profile set")
		return
	}
	profileSet.Update(status.CurrentSecurityLevel())

	// check for any network access
	if !profileSet.CheckFlag(profile.Internet) && !profileSet.CheckFlag(profile.LAN) {
		log.Infof("firewall: denying connection %s, accessing Internet or LAN not allowed", connection)
		connection.Deny("accessing Internet or LAN not allowed")
		return
	}

	// check domain list
	permitted, ok := profileSet.CheckDomain(fqdn)
	if ok {
		if permitted {
			log.Infof("firewall: accepting connection %s, domain is whitelisted", connection)
			connection.Accept("domain is whitelisted")
		} else {
			log.Infof("firewall: denying connection %s, domain is blacklisted", connection)
			connection.Deny("domain is blacklisted")
		}
		return
	}

	switch profileSet.GetProfileMode() {
	case profile.Whitelist:
		log.Infof("firewall: denying connection %s, domain is not whitelisted", connection)
		connection.Deny("domain is not whitelisted")
	case profile.Prompt:

		// check Related flag
		// TODO: improve this!
		if profileSet.CheckFlag(profile.Related) {
			matched := false
			pathElements := strings.Split(connection.Process().Path, "/") // FIXME: path seperator
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
				if levenshtein.Match(domainElement, connection.Process().Name, nil) > 0.5 {
					matched = true
					processElement = connection.Process().Name
					break matchLoop
				}
				if levenshtein.Match(domainElement, connection.Process().ExecName, nil) > 0.5 {
					matched = true
					processElement = connection.Process().ExecName
					break matchLoop
				}
			}

			if matched {
				log.Infof("firewall: accepting connection %s, match to domain was found: %s ~= %s", connection, domainElement, processElement)
				connection.Accept("domain is related to process")
			}
		}

		if connection.GetVerdict() != network.ACCEPT {
			// TODO
			log.Infof("firewall: accepting connection %s, domain permitted (prompting is not yet implemented)", connection)
			connection.Accept("domain permitted (prompting is not yet implemented)")
		}

	case profile.Blacklist:
		log.Infof("firewall: accepting connection %s, domain is not blacklisted", connection)
		connection.Accept("domain is not blacklisted")
	}

}

// DecideOnConnectionAfterIntel makes a decision about a connection after the dns query is resolved and intel is gathered.
func DecideOnConnectionAfterIntel(connection *network.Connection, fqdn string, rrCache *intel.RRCache) *intel.RRCache {

	// grant self
	if connection.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own connection %s", connection)
		connection.Accept("")
		return rrCache
	}

	// check if there is a profile
	profileSet := connection.Process().ProfileSet()
	if profileSet == nil {
		log.Errorf("firewall: denying connection %s, no profile set", connection)
		connection.Deny("no profile")
		return rrCache
	}
	profileSet.Update(status.CurrentSecurityLevel())

	// TODO: Stamp integration

	// TODO: Gate17 integration
	// tunnelInfo, err := AssignTunnelIP(fqdn)

	rrCache.Duplicate().FilterEntries(profileSet.CheckFlag(profile.Internet), profileSet.CheckFlag(profile.LAN), false)
	if len(rrCache.Answer) == 0 {
		if profileSet.CheckFlag(profile.Internet) {
			connection.Deny("server is located in the LAN, but LAN access is not permitted")
		} else {
			connection.Deny("server is located in the Internet, but Internet access is not permitted")
		}
	}

	return rrCache
}

// DeciceOnConnection makes a decision about a connection with its first packet.
func DecideOnConnection(connection *network.Connection, pkt packet.Packet) {

	// grant self
	if connection.Process().Pid == os.Getpid() {
		log.Infof("firewall: granting own connection %s", connection)
		connection.Accept("")
		return
	}

	// check if there is a profile
	profileSet := connection.Process().ProfileSet()
	if profileSet == nil {
		log.Errorf("firewall: denying connection %s, no profile set", connection)
		connection.Deny("no profile")
		return
	}
	profileSet.Update(status.CurrentSecurityLevel())

	// check connection type
	switch connection.Domain {
	case network.IncomingHost, network.IncomingLAN, network.IncomingInternet, network.IncomingInvalid:
		if !profileSet.CheckFlag(profile.Service) {
			log.Infof("firewall: denying connection %s, not a service", connection)
			if connection.Domain == network.IncomingHost {
				connection.Block("not a service")
			} else {
				connection.Drop("not a service")
			}
			return
		}
	case network.PeerLAN, network.PeerInternet, network.PeerInvalid: // Important: PeerHost is and should be missing!
		if !profileSet.CheckFlag(profile.PeerToPeer) {
			log.Infof("firewall: denying connection %s, peer to peer connections (to an IP) not allowed", connection)
			connection.Deny("peer to peer connections (to an IP) not allowed")
			return
		}
	}

	// check network scope
	switch connection.Domain {
	case network.IncomingHost:
		if !profileSet.CheckFlag(profile.Localhost) {
			log.Infof("firewall: denying connection %s, serving localhost not allowed", connection)
			connection.Block("serving localhost not allowed")
			return
		}
	case network.IncomingLAN:
		if !profileSet.CheckFlag(profile.LAN) {
			log.Infof("firewall: denying connection %s, serving LAN not allowed", connection)
			connection.Deny("serving LAN not allowed")
			return
		}
	case network.IncomingInternet:
		if !profileSet.CheckFlag(profile.Internet) {
			log.Infof("firewall: denying connection %s, serving Internet not allowed", connection)
			connection.Deny("serving Internet not allowed")
			return
		}
	case network.IncomingInvalid:
		log.Infof("firewall: denying connection %s, invalid IP address", connection)
		connection.Drop("invalid IP address")
		return
	case network.PeerHost:
		if !profileSet.CheckFlag(profile.Localhost) {
			log.Infof("firewall: denying connection %s, accessing localhost not allowed", connection)
			connection.Block("accessing localhost not allowed")
			return
		}
	case network.PeerLAN:
		if !profileSet.CheckFlag(profile.LAN) {
			log.Infof("firewall: denying connection %s, accessing the LAN not allowed", connection)
			connection.Deny("accessing the LAN not allowed")
			return
		}
	case network.PeerInternet:
		if !profileSet.CheckFlag(profile.Internet) {
			log.Infof("firewall: denying connection %s, accessing the Internet not allowed", connection)
			connection.Deny("accessing the Internet not allowed")
			return
		}
	case network.PeerInvalid:
		log.Infof("firewall: denying connection %s, invalid IP address", connection)
		connection.Deny("invalid IP address")
		return
	}

	log.Infof("firewall: accepting connection %s", connection)
	connection.Accept("")
}

// DecideOnLink makes a decision about a link with the first packet.
func DecideOnLink(connection *network.Connection, link *network.Link, pkt packet.Packet) {
	// check:
	// Profile.Flags
	// - network specific: Internet, LocalNet
	// Profile.ConnectPorts
	// Profile.ListenPorts

	// check if there is a profile
	profileSet := connection.Process().ProfileSet()
	if profileSet == nil {
		log.Infof("firewall: no profile, denying %s", link)
		link.Block("no profile")
		return
	}
	profileSet.Update(status.CurrentSecurityLevel())

	// get remote Port
	protocol := pkt.GetIPHeader().Protocol
	var dstPort uint16
	tcpUDPHeader := pkt.GetTCPUDPHeader()
	if tcpUDPHeader != nil {
		dstPort = tcpUDPHeader.DstPort
	}

	// check port list
	permitted, ok := profileSet.CheckPort(connection.Direction, uint8(protocol), dstPort)
	if ok {
		if permitted {
			log.Infof("firewall: accepting link %s", link)
			link.Accept("port whitelisted")
		} else {
			log.Infof("firewall: denying link %s: port %d is blacklisted", link, dstPort)
			link.Deny("port blacklisted")
		}
		return
	}

	switch profileSet.GetProfileMode() {
	case profile.Whitelist:
		log.Infof("firewall: denying link %s: port %d is not whitelisted", link, dstPort)
		link.Deny("port is not whitelisted")
		return
	case profile.Prompt:
		log.Infof("firewall: accepting link %s: port %d is blacklisted", link, dstPort)
		link.Accept("port permitted (prompting is not yet implemented)")
		return
	case profile.Blacklist:
		log.Infof("firewall: accepting link %s: port %d is not blacklisted", link, dstPort)
		link.Accept("port is not blacklisted")
		return
	}

	log.Infof("firewall: accepting link %s", link)
	link.Accept("")
}
