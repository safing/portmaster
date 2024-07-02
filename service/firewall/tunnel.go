package firewall

import (
	"context"
	"errors"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/crew"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/sluice"
)

func checkTunneling(ctx context.Context, conn *network.Connection) {
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
	case conn.IPProtocol != packet.TCP && conn.IPProtocol != packet.UDP:
		// Unsupported protocol.
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
		conn.ProfileRevisionCounter = layeredProfile.Update(
			conn.Process().MatchingData(),
			conn.Process().CreateProfileCallback,
		)
		conn.SaveWhenFinished()
	} else {
		// Check if the revision counter of the connection needs updating.
		revCnt := layeredProfile.RevisionCnt()
		if conn.ProfileRevisionCounter != revCnt {
			conn.ProfileRevisionCounter = revCnt
			conn.SaveWhenFinished()
		}
	}

	// Check if tunneling is enabled for this app at all.
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
		log.Tracer(ctx).Trace("SPN not ready for tunneling")
		conn.Failed("SPN not ready for tunneling", "")
		return
	}

	conn.SetVerdictDirectly(network.VerdictRerouteToTunnel)
	conn.Tunneled = true
}

func requestTunneling(ctx context.Context, conn *network.Connection) error {
	// Get profile.
	layeredProfile := conn.Process().Profile()
	if layeredProfile == nil {
		return errors.New("no profile set")
	}

	// Get tunnel options.
	conn.TunnelOpts = DeriveTunnelOptions(layeredProfile, conn.Process(), conn.Entity, conn.Encrypted)

	// Queue request in sluice.
	err := sluice.AwaitRequest(conn, crew.HandleSluiceRequest)
	if err != nil {
		return err
	}

	log.Tracer(ctx).Trace("filter: tunneling requested")
	return nil
}

func init() {
	navigator.DeriveTunnelOptions = func(lp *profile.LayeredProfile, destination *intel.Entity, connEncrypted bool) *navigator.Options {
		return DeriveTunnelOptions(lp, nil, destination, connEncrypted)
	}
}

// DeriveTunnelOptions derives and returns the tunnel options from the connection and profile.
func DeriveTunnelOptions(lp *profile.LayeredProfile, proc *process.Process, destination *intel.Entity, connEncrypted bool) *navigator.Options {
	// Set options.
	tunnelOpts := &navigator.Options{
		Transit: &navigator.TransitHubOptions{
			HubPolicies: lp.StackedTransitHubPolicies(),
		},
		Destination: &navigator.DestinationHubOptions{
			HubPolicies:        lp.StackedExitHubPolicies(),
			CheckHubPolicyWith: destination,
		},
		RoutingProfile: lp.SPNRoutingAlgorithm(),
	}
	if !connEncrypted {
		tunnelOpts.Destination.Regard = tunnelOpts.Destination.Regard.Add(navigator.StateTrusted)
		// TODO: Add this when all Hubs are on v0.6.21+
		// tunnelOpts.Destination.Regard = tunnelOpts.Destination.Regard.Add(navigator.StateAllowUnencrypted)
	}

	// Add required verified owners if community nodes should not be used.
	if !useCommunityNodes() {
		tunnelOpts.Transit.RequireVerifiedOwners = captain.NonCommunityVerifiedOwners
		tunnelOpts.Destination.RequireVerifiedOwners = captain.NonCommunityVerifiedOwners
	}

	// Get routing profile for checking for upgrades.
	routingProfile := navigator.GetRoutingProfile(tunnelOpts.RoutingProfile)

	// If we have any exit hub policies, we must be able to hop in order to follow the policy.
	// Switch to single-hop routing to allow for routing with hub selection.
	if routingProfile.MaxHops <= 1 && navigator.HubPoliciesAreSet(tunnelOpts.Destination.HubPolicies) {
		tunnelOpts.RoutingProfile = navigator.RoutingProfileSingleHopID
	}

	// If the current home node is not trusted, then upgrade at least to two hops.
	if routingProfile.MinHops < 2 {
		homeNode, _ := navigator.Main.GetHome()
		if homeNode != nil && !homeNode.State.Has(navigator.StateTrusted) {
			tunnelOpts.RoutingProfile = navigator.RoutingProfileDoubleHopID
		}
	}

	// Special handling for the internal DNS resolver.
	if proc != nil && proc.Pid == ownPID && resolver.IsResolverAddress(destination.IP, destination.Port) {
		dnsExitHubPolicy, err := captain.GetDNSExitHubPolicy()
		if err != nil {
			log.Errorf("firewall: failed to get dns exit hub policy: %s", err)
		}

		if err == nil && dnsExitHubPolicy.IsSet() {
			// Apply the dns exit hub policy, if set.
			tunnelOpts.Destination.HubPolicies = []endpoints.Endpoints{dnsExitHubPolicy}
			// Use the routing algorithm from the profile, as the home profile won't work with the policy.
			tunnelOpts.RoutingProfile = lp.SPNRoutingAlgorithm()
			// Raise the routing algorithm at least to single-hop.
			if tunnelOpts.RoutingProfile == navigator.RoutingProfileHomeID {
				tunnelOpts.RoutingProfile = navigator.RoutingProfileSingleHopID
			}
		} else {
			// Disable any policies for the internal DNS resolver.
			tunnelOpts.Destination.HubPolicies = nil
			// Always use the home routing profile for the internal DNS resolver.
			tunnelOpts.RoutingProfile = navigator.RoutingProfileHomeID
		}
	}

	return tunnelOpts
}
