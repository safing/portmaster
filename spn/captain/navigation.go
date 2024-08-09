package captain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/terminal"
)

const stopCraneAfterBeingUnsuggestedFor = 6 * time.Hour

var (
	// ErrAllHomeHubsExcluded is returned when all available home hubs were excluded.
	ErrAllHomeHubsExcluded = errors.New("all home hubs are excluded")

	// ErrReInitSPNSuggested is returned when no home hub can be found, even without rules.
	ErrReInitSPNSuggested = errors.New("SPN re-init suggested")
)

func establishHomeHub(ctx *mgr.WorkerCtx) error {
	// Get own IP.
	locations, ok := netenv.GetInternetLocation()
	if !ok || len(locations.All) == 0 {
		return errors.New("failed to locate own device")
	}
	log.Debugf(
		"spn/captain: looking for new home hub near %s and %s",
		locations.BestV4(),
		locations.BestV6(),
	)

	// Get own entity.
	// Checking the entity against the entry policies is somewhat hit and miss
	// anyway, as the device location is an approximation.
	var myEntity *intel.Entity
	if dl := locations.BestV4(); dl != nil && dl.IP != nil {
		myEntity = (&intel.Entity{IP: dl.IP}).Init(0)
		myEntity.FetchData(ctx.Ctx())
	} else if dl := locations.BestV6(); dl != nil && dl.IP != nil {
		myEntity = (&intel.Entity{IP: dl.IP}).Init(0)
		myEntity.FetchData(ctx.Ctx())
	}

	// Get home hub policy for selecting the home hub.
	homePolicy, err := getHomeHubPolicy()
	if err != nil {
		return err
	}

	// Build navigation options for searching for a home hub.
	opts := &navigator.Options{
		Home: &navigator.HomeHubOptions{
			HubPolicies:        []endpoints.Endpoints{homePolicy},
			CheckHubPolicyWith: myEntity,
		},
	}

	// Add requirement to only use Safing nodes when not using community nodes.
	if !cfgOptionUseCommunityNodes() {
		opts.Home.RequireVerifiedOwners = NonCommunityVerifiedOwners
	}

	// Require a trusted home node when the routing profile requires less than two hops.
	routingProfile := navigator.GetRoutingProfile(cfgOptionRoutingAlgorithm())
	if routingProfile.MinHops < 2 {
		opts.Home.Regard = opts.Home.Regard.Add(navigator.StateTrusted)
	}

	// Find nearby hubs.
findCandidates:
	candidates, err := navigator.Main.FindNearestHubs(
		locations.BestV4().LocationOrNil(),
		locations.BestV6().LocationOrNil(),
		opts, navigator.HomeHub,
	)
	if err != nil {
		switch {
		case errors.Is(err, navigator.ErrEmptyMap):
			// bootstrap to the network!
			err := bootstrapWithUpdates()
			if err != nil {
				return err
			}
			goto findCandidates

		case errors.Is(err, navigator.ErrAllPinsDisregarded):
			if len(homePolicy) > 0 {
				return ErrAllHomeHubsExcluded
			}
			return ErrReInitSPNSuggested

		default:
			return fmt.Errorf("failed to find nearby hubs: %w", err)
		}
	}

	// Try connecting to a hub.
	var tries int
	var candidate *hub.Hub
	for tries, candidate = range candidates {
		err = connectToHomeHub(ctx, candidate)
		if err != nil {
			// Check if context is canceled.
			if ctx.IsDone() {
				return ctx.Ctx().Err()
			}
			// Check if the SPN protocol is stopping again.
			if errors.Is(err, terminal.ErrStopping) {
				return err
			}
			log.Warningf("spn/captain: failed to connect to %s as new home: %s", candidate, err)
		} else {
			log.Infof("spn/captain: established connection to %s as new home with %d failed tries", candidate, tries)
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("failed to connect to a new home hub - tried %d hubs: %w", tries+1, err)
	}
	return errors.New("no home hub candidates available")
}

func connectToHomeHub(wCtx *mgr.WorkerCtx, dst *hub.Hub) error {
	// Create new context with timeout.
	// The maximum timeout is a worst case safeguard.
	// Keep in mind that multiple IPs and protocols may be tried in all configurations.
	// Some servers will be (possibly on purpose) hard to reach.
	ctx, cancel := context.WithTimeout(wCtx.Ctx(), 5*time.Minute)
	defer cancel()

	// Set and clean up exceptions.
	setExceptions(dst.Info.IPv4, dst.Info.IPv6)
	defer setExceptions(nil, nil)

	// Connect to hub.
	crane, err := EstablishCrane(ctx, dst)
	if err != nil {
		return err
	}

	// Cleanup connection in case of failure.
	var success bool
	defer func() {
		if !success {
			crane.Stop(nil)
		}
	}()

	// Query all gossip msgs on first connection.
	gossipQuery, tErr := NewGossipQueryOp(crane.Controller)
	if tErr != nil {
		log.Warningf("spn/captain: failed to start initial gossip query: %s", tErr)
	}
	// Wait for gossip query to complete.
	select {
	case <-gossipQuery.ctx.Done():
	case <-ctx.Done():
		return context.Canceled
	}

	// Create communication terminal.
	homeTerminal, initData, tErr := docks.NewLocalCraneTerminal(crane, nil, terminal.DefaultHomeHubTerminalOpts())
	if tErr != nil {
		return tErr.Wrap("failed to create home terminal")
	}
	tErr = crane.EstablishNewTerminal(homeTerminal, initData)
	if tErr != nil {
		return tErr.Wrap("failed to connect home terminal")
	}

	if !DisableAccount {
		// Authenticate to home hub.
		authOp, tErr := access.AuthorizeToTerminal(homeTerminal)
		if tErr != nil {
			return tErr.Wrap("failed to authorize")
		}
		select {
		case tErr := <-authOp.Result:
			if !tErr.Is(terminal.ErrExplicitAck) {
				return tErr.Wrap("failed to authenticate to")
			}
		case <-time.After(3 * time.Second):
			return terminal.ErrTimeout.With("waiting for auth to complete")
		case <-ctx.Done():
			return terminal.ErrStopping
		}
	}

	// Set new home on map.
	ok := navigator.Main.SetHome(dst.ID, homeTerminal)
	if !ok {
		return errors.New("failed to set home hub on map")
	}

	// Assign crane to home hub in order to query it later.
	docks.AssignCrane(crane.ConnectedHub.ID, crane)

	success = true
	return nil
}

func optimizeNetwork(ctx *mgr.WorkerCtx) error {
	if publicIdentity == nil {
		return nil
	}

optimize:
	result, err := navigator.Main.Optimize(nil)
	if err != nil {
		if errors.Is(err, navigator.ErrEmptyMap) {
			// bootstrap to the network!
			err := bootstrapWithUpdates()
			if err != nil {
				return err
			}
			goto optimize
		}

		return err
	}

	// Create any new connections.
	var createdConnections int
	var attemptedConnections int
	for _, connectTo := range result.SuggestedConnections {
		// Skip duplicates.
		if connectTo.Duplicate {
			continue
		}

		// Check if connection already exists.
		crane := docks.GetAssignedCrane(connectTo.Hub.ID)
		if crane != nil {
			// Update last suggested timestamp.
			crane.NetState.UpdateLastSuggestedAt()
			// Continue crane if stopping.
			if crane.AbortStopping() {
				log.Infof("spn/captain: optimization aborted retiring of %s, removed stopping mark", crane)
				crane.NotifyUpdate()
			}

			// Create new connections if we have connects left.
		} else if createdConnections < result.MaxConnect {
			attemptedConnections++

			crane, tErr := EstablishPublicLane(ctx.Ctx(), connectTo.Hub)
			if !tErr.IsOK() {
				log.Warningf("spn/captain: failed to establish lane to %s: %s", connectTo.Hub, tErr)
			} else {
				createdConnections++
				crane.NetState.UpdateLastSuggestedAt()

				log.Infof("spn/captain: established lane to %s", connectTo.Hub)
			}
		}
	}

	// Log optimization result.
	if attemptedConnections > 0 {
		log.Infof(
			"spn/captain: created %d/%d new connections for %s optimization",
			createdConnections,
			attemptedConnections,
			result.Purpose)
	} else {
		log.Infof(
			"spn/captain: checked %d connections for %s optimization",
			len(result.SuggestedConnections),
			result.Purpose,
		)
	}

	// Retire cranes if unsuggested for a while.
	if result.StopOthers {
		for _, crane := range docks.GetAllAssignedCranes() {
			switch {
			case crane.Stopped():
				// Crane already stopped.
			case crane.IsStopping():
				// Crane is stopping, forcibly stop if mine and suggested.
				if crane.IsMine() && crane.NetState.StopSuggested() {
					crane.Stop(nil)
				}
			case crane.IsMine() && crane.NetState.StoppingSuggested():
				// Mark as stopping if mine and suggested.
				crane.MarkStopping()
			case crane.NetState.RequestStoppingSuggested(stopCraneAfterBeingUnsuggestedFor):
				// Mark as stopping requested.
				crane.MarkStoppingRequested()
			}
		}
	}

	return nil
}
