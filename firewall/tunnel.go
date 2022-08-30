package firewall

import (
	"context"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/resolver"
	"github.com/safing/spn/captain"
	"github.com/safing/spn/crew"
	"github.com/safing/spn/navigator"
	"github.com/safing/spn/sluice"
)

func checkTunneling(ctx context.Context, conn *network.Connection, pkt packet.Packet) {
	// Check if the connection should be tunneled at all.
	switch {
	case !tunnelEnabled():
		// Tunneling is disabled.
		return
	case !conn.Entity.IPScope.IsGlobal():
		// Can't tunnel Local/LAN connections.
		return
	case conn.Inbound:
		// Can't tunnel incoming connections.
		return
	case conn.Verdict != network.VerdictAccept:
		// Connection will be blocked.
		return
	case conn.Process().Pid == ownPID:
		// Bypass tunneling for certain own connections.
		switch {
		case !captain.ClientReady():
			return
		case captain.IsExcepted(conn.Entity.IP):
			return
		}
	}

	// Check more extensively for Local/LAN connections.
	localNet, err := netenv.GetLocalNetwork(conn.Entity.IP)
	if err != nil {
		log.Warningf("firewall: failed to check if %s is in my net: %s", conn.Entity.IP, err)
	} else if localNet != nil {
		// With IPv6, just checking the IP scope is not enough, as the host very
		// likely has a public IPv6 address.
		// Don't tunnel LAN connections.

		// TODO: We currently don't check the full LAN scope, but only the
		// broadcast domain of the host - ie. the networks that the host is
		// directly attached to.
		return
	}

	// Get profile.
	layeredProfile := conn.Process().Profile()
	if layeredProfile == nil {
		conn.Failed("no profile set", "")
		return
	}

	// Update profile.
	if layeredProfile.NeedsUpdate() {
		// Update revision counter in connection.
		conn.ProfileRevisionCounter = layeredProfile.Update()
		conn.SaveWhenFinished()
	} else {
		// Check if the revision counter of the connection needs updating.
		revCnt := layeredProfile.RevisionCnt()
		if conn.ProfileRevisionCounter != revCnt {
			conn.ProfileRevisionCounter = revCnt
			conn.SaveWhenFinished()
		}
	}

	// Check if tunneling is enabeld for this app at all.
	if !layeredProfile.UseSPN() {
		return
	}

	// Check if tunneling is enabled for entity.
	conn.Entity.FetchData(ctx)
	result, _ := layeredProfile.MatchSPNUsagePolicy(ctx, conn.Entity)
	switch result {
	case endpoints.MatchError:
		conn.Failed("failed to check SPN rules", profile.CfgOptionSPNUsagePolicyKey)
		return
	case endpoints.Denied:
		return
	case endpoints.Permitted, endpoints.NoMatch:
		// Continue
	}

	// Tunnel all the things!
	conn.SaveWhenFinished()

	// Check if ready.
	if !captain.ClientReady() {
		// Block connection as SPN is not ready yet.
		log.Tracer(pkt.Ctx()).Trace("SPN not ready for tunneling")
		conn.Failed("SPN not ready for tunneling", "")
		return
	}

	// Set options.
	conn.TunnelOpts = &navigator.Options{
		HubPolicies:                   layeredProfile.StackedExitHubPolicies(),
		CheckHubExitPolicyWith:        conn.Entity,
		RequireTrustedDestinationHubs: !conn.Encrypted,
		RoutingProfile:                layeredProfile.SPNRoutingAlgorithm(),
	}

	// Add required verified owners if community nodes should not be used.
	if !useCommunityNodes() {
		conn.TunnelOpts.RequireVerifiedOwners = captain.NonCommunityVerifiedOwners
	}

	// If we have any exit hub policies, we need to raise the routing algorithm at least to single-hop.
	if conn.TunnelOpts.RoutingProfile == navigator.RoutingProfileHomeID &&
		conn.TunnelOpts.HubPoliciesAreSet() {
		conn.TunnelOpts.RoutingProfile = navigator.RoutingProfileSingleHopID
	}

	// Special handling for the internal DNS resolver.
	if conn.Process().Pid == ownPID && resolver.IsResolverAddress(conn.Entity.IP, conn.Entity.Port) {
		dnsExitHubPolicy, err := captain.GetDNSExitHubPolicy()
		if err != nil {
			log.Errorf("firewall: failed to get dns exit hub policy: %s", err)
		}

		if err == nil && dnsExitHubPolicy.IsSet() {
			// Apply the dns exit hub policy, if set.
			conn.TunnelOpts.HubPolicies = []endpoints.Endpoints{dnsExitHubPolicy}
			// Use the routing algorithm from the profile, as the home profile won't work with the policy.
			conn.TunnelOpts.RoutingProfile = layeredProfile.SPNRoutingAlgorithm()
			// Raise the routing algorithm at least to single-hop.
			if conn.TunnelOpts.RoutingProfile == navigator.RoutingProfileHomeID {
				conn.TunnelOpts.RoutingProfile = navigator.RoutingProfileSingleHopID
			}
		} else {
			// Disable any policies for the internal DNS resolver.
			conn.TunnelOpts.HubPolicies = nil
			// Always use the home routing profile for the internal DNS resolver.
			conn.TunnelOpts.RoutingProfile = navigator.RoutingProfileHomeID
		}
	}

	// Queue request in sluice.
	err = sluice.AwaitRequest(conn, crew.HandleSluiceRequest)
	if err != nil {
		log.Tracer(pkt.Ctx()).Warningf("failed to request tunneling: %s", err)
		conn.Failed("failed to request tunneling", "")
	} else {
		log.Tracer(pkt.Ctx()).Trace("filter: tunneling requested")
		conn.Verdict = network.VerdictRerouteToTunnel
		conn.Tunneled = true
	}
}
