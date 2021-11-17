package compat

import (
	"net"

	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
)

func SubmitSystemIntegrationCheckPacket(p packet.Packet) {
	select {
	case systemIntegrationCheckPackets <- p:
	default:
	}
}

func SubmitDNSCheckDomain(subdomain string) (respondWith net.IP) {
	// Submit queried domain.
	select {
	case dnsCheckReceivedDomain <- subdomain:
	default:
	}

	// Return the answer.
	dnsCheckAnswerLock.Lock()
	defer dnsCheckAnswerLock.Unlock()
	return dnsCheckAnswer
}

func ReportSecureDNSBypassIssue(p *process.Process) {
	secureDNSBypassIssue.notify(p)
}

func ReportMultiPeerUDPTunnelIssue(p *process.Process) {
	multiPeerUDPTunnelIssue.notify(p)
}
