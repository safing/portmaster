package compat

import (
	"net"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
)

// SubmitSystemIntegrationCheckPacket submit a packet for the system integrity check.
func SubmitSystemIntegrationCheckPacket(p packet.Packet) {
	select {
	case systemIntegrationCheckPackets <- p:
	default:
	}
}

// SubmitDNSCheckDomain submits a subdomain for the dns check.
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

// ReportSecureDNSBypassIssue reports a DNS bypassing issue for the given process.
func ReportSecureDNSBypassIssue(p *process.Process) {
	secureDNSBypassIssue.notify(p)
}

// ReportMultiPeerUDPTunnelIssue reports a multi-peer UDP tunnel for the given process.
func ReportMultiPeerUDPTunnelIssue(p *process.Process) {
	multiPeerUDPTunnelIssue.notify(p)
}
