package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/spn/navigator"
)

// NewDefaultConnection creates a new connection with default values except local and remote IPs and protocols.
func NewDefaultConnection(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, ipVersion packet.IPVersion, protocol packet.IPProtocol) *Connection {
	connInfo := &Connection{
		ID:           fmt.Sprintf("%s-%s-%d-%s-%d", protocol.String(), localIP, localPort, remoteIP, remotePort),
		Type:         IPConnection,
		External:     false,
		IPVersion:    ipVersion,
		Inbound:      false,
		IPProtocol:   protocol,
		LocalIP:      localIP,
		LocalIPScope: netutils.Global,
		LocalPort:    localPort,
		Entity: &intel.Entity{
			Protocol: uint8(protocol),
			IP:       remoteIP,
			Port:     remotePort,
		},
		Resolver:         nil,
		Started:          time.Now().Unix(),
		VerdictPermanent: false,
		Tunneled:         true,
		Encrypted:        false,
		Internal:         false,
		addedToMetrics:   true, // Metrics are not needed for now. This will mark the Connection to be ignored.
		process:          process.GetUnidentifiedProcess(context.Background()),
	}

	// TODO: Quick fix for the SPN.
	// Use inspection framework for proper encryption detection.
	switch connInfo.Entity.DstPort() {
	case
		22,  // SSH
		443, // HTTPS
		465, // SMTP-SSL
		853, // DoT
		993, // IMAP-SSL
		995: // POP3-SSL
		connInfo.Encrypted = true
	}

	var layeredProfile = connInfo.process.Profile()
	connInfo.TunnelOpts = &navigator.Options{
		HubPolicies:                   layeredProfile.StackedExitHubPolicies(),
		CheckHubExitPolicyWith:        connInfo.Entity,
		RequireTrustedDestinationHubs: !connInfo.Encrypted,
		RoutingProfile:                layeredProfile.SPNRoutingAlgorithm(),
	}

	return connInfo
}
