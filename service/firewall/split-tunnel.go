package firewall

import (
	"context"
	"errors"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/service/splittun"
)

func checkSplitTunneling(ctx context.Context, conn *network.Connection) {
	// Check if the connection should be tunneled at all.
	switch {
	case conn.Entity.IPScope.IsLocalhost():
		// Can't tunnel Local connections.
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
		// Bypass tunneling for own connections.
		return
	case !splittun.IsReady():
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

	// Check if split-tunneling is enabled for this app at all.
	if !layeredProfile.UseSplitTun() {
		return
	}

	// Check if tunneling is enabled for entity.
	conn.Entity.FetchData(ctx)
	result, _ := layeredProfile.MatchSplitTunUsagePolicy(ctx, conn.Entity)
	switch result {
	case endpoints.MatchError:
		conn.Failed("failed to check Split Tunnel rules", profile.CfgOptionSplitTunUsagePolicyKey)
		return
	case endpoints.Denied:
		return
	case endpoints.Permitted, endpoints.NoMatch:
	}

	conn.SaveWhenFinished()

	conn.SetVerdictDirectly(network.VerdictRerouteToSplitTun)
}

func requestSplitTunneling(ctx context.Context, conn *network.Connection) error {
	// Get profile.
	layeredProfile := conn.Process().Profile()
	if layeredProfile == nil {
		return errors.New("no profile set")
	}

	interfaceToBind := layeredProfile.SplitTunInterface()

	// Queue request in splittun module.
	splitTunCtx, err := splittun.AwaitRequest(conn, interfaceToBind)
	if err != nil {
		return err
	}

	// Store context on the connection so the UI can display interface information.
	conn.SplitTunContext = splitTunCtx

	log.Tracer(ctx).Trace("filter: split tunneling requested")
	return nil
}

func isOwnSplitTunnelProxyConnection(conn *network.Connection) bool {
	switch {
	case conn.Process().Pid != ownPID:
		// Proxies are running only in our own process.
		return false
	case conn.Entity.IPScope.IsLocalhost():
		// Local connections are not proxied.
		return false
	case conn.IPProtocol != packet.TCP && conn.IPProtocol != packet.UDP:
		// Unsupported protocol.
		return false
	case !splittun.IsReady():
		return false
	}

	return splittun.IsProxiedConnectionInfo(conn)
}
