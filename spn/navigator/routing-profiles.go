package navigator

import (
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile"
)

// RoutingProfile defines a routing algorithm with some options.
type RoutingProfile struct {
	ID string

	// Name is the human readable name of the profile.
	Name string

	// MinHops defines how many hops a route must have at minimum. In order to
	// reduce confusion, the Home Hub is also counted.
	MinHops int

	// MaxHops defines the limit on how many hops a route may have. In order to
	// reduce confusion, the Home Hub is also counted.
	MaxHops int

	// MaxExtraHops sets a limit on how many extra hops are allowed in addition
	// to the amount of Hops in the currently best route. This is an optimization
	// option and should not interfere with finding the best route, but might
	// reduce the amount of routes found.
	MaxExtraHops int

	// MaxExtraCost sets a limit on the extra cost allowed in addition to the
	// cost of the currently best route. This is an optimization option and
	// should not interfere with finding the best route, but might reduce the
	// amount of routes found.
	MaxExtraCost float32
}

// Routing Profile Names.
const (
	RoutingProfileHomeID      = "home"
	RoutingProfileSingleHopID = "single-hop"
	RoutingProfileDoubleHopID = "double-hop"
	RoutingProfileTripleHopID = "triple-hop"
)

// Routing Profiles.
var (
	DefaultRoutingProfileID = profile.DefaultRoutingProfileID

	RoutingProfileHome = &RoutingProfile{
		ID:      "home",
		Name:    "Plain VPN Mode",
		MinHops: 1,
		MaxHops: 1,
	}
	RoutingProfileSingleHop = &RoutingProfile{
		ID:           "single-hop",
		Name:         "Speed Focused",
		MinHops:      1,
		MaxHops:      3,
		MaxExtraHops: 1,
		MaxExtraCost: 10000,
	}
	RoutingProfileDoubleHop = &RoutingProfile{
		ID:           "double-hop",
		Name:         "Balanced",
		MinHops:      2,
		MaxHops:      4,
		MaxExtraHops: 2,
		MaxExtraCost: 10000,
	}
	RoutingProfileTripleHop = &RoutingProfile{
		ID:           "triple-hop",
		Name:         "Privacy Focused",
		MinHops:      3,
		MaxHops:      5,
		MaxExtraHops: 3,
		MaxExtraCost: 10000,
	}
)

// GetRoutingProfile returns the routing profile with the given ID.
func GetRoutingProfile(id string) *RoutingProfile {
	switch id {
	case RoutingProfileHomeID:
		return RoutingProfileHome
	case RoutingProfileSingleHopID:
		return RoutingProfileSingleHop
	case RoutingProfileDoubleHopID:
		return RoutingProfileDoubleHop
	case RoutingProfileTripleHopID:
		return RoutingProfileTripleHop
	default:
		return RoutingProfileDoubleHop
	}
}

type routeCompliance uint8

const (
	routeOk           routeCompliance = iota // Route is fully compliant and can be used.
	routeNonCompliant                        // Route is not compliant, but this might change if more hops are added.
	routeDisqualified                        // Route is disqualified and won't be able to become compliant.
)

func (rp *RoutingProfile) checkRouteCompliance(route *Route, foundRoutes *Routes) routeCompliance {
	switch {
	case len(route.Path) < rp.MinHops:
		// Route is shorter than the defined minimum.
		return routeNonCompliant
	case len(route.Path) > rp.MaxHops:
		// Route is longer than the defined maximum.
		return routeDisqualified
	}

	// Check for hub re-use.
	if len(route.Path) >= 2 {
		lastHop := route.Path[len(route.Path)-1]
		for _, hop := range route.Path[:len(route.Path)-1] {
			if lastHop.pin.Hub.ID == hop.pin.Hub.ID {
				return routeDisqualified
			}
		}
	}

	// Check if hub is already in use, if so check if the route matches.
	if len(route.Path) >= 2 {
		// Get active connection to the last pin of the current path.
		lastPinConnection := route.Path[len(route.Path)-1].pin.Connection

		switch {
		case lastPinConnection == nil:
			// Last pin is not yet connected.
		case len(lastPinConnection.Route.Path) < 2:
			// Path of last pin does not have enough hops.
			// This is unexpected and should not happen.
			log.Errorf(
				"navigator: expected active connection to %s to have 2 hops or more on path, but it had %d",
				route.Path[len(route.Path)-1].pin.Hub.StringWithoutLocking(),
				len(lastPinConnection.Route.Path),
			)
		case lastPinConnection.Route.Path[len(lastPinConnection.Route.Path)-2].pin.Hub.ID != route.Path[len(route.Path)-2].pin.Hub.ID:
			// The previous hop of the existing route and the one we are evaluating don't match.
			// Currently, we only allow one session per Hub.
			return routeDisqualified
		}
	}

	// Abort route exploration when we are outside the optimization boundaries.
	if len(foundRoutes.All) > 0 {
		// Get the best found route.
		best := foundRoutes.All[0]
		// Abort if current route exceeds max extra costs.
		if route.TotalCost > best.TotalCost+rp.MaxExtraCost {
			return routeDisqualified
		}
		// Abort if current route exceeds max extra hops.
		if len(route.Path) > len(best.Path)+rp.MaxExtraHops {
			return routeDisqualified
		}
	}

	return routeOk
}
