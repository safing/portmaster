package navigator

import (
	"bytes"
	"errors"
	"fmt"
	mrand "math/rand"
	"net"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
)

func registerRouteAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/route/to/{destination:[a-z0-9_\.:-]{1,255}}`,
		Read:        api.PermitUser,
		ActionFunc:  handleRouteCalculationRequest,
		Name:        "Calculate Route through SPN",
		Description: "Returns a textual representation of the routing process.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "profile",
				Value:       "<id>|global",
				Description: "Specify a profile ID to load more settings for simulation.",
			},
			{
				Method:      http.MethodGet,
				Field:       "encrypted",
				Value:       "true",
				Description: "Specify to signify that the simulated connection should be regarded as encrypted. Only valid with a profile.",
			},
		},
	}); err != nil {
		return err
	}

	return nil
}

func handleRouteCalculationRequest(ar *api.Request) (msg string, err error) { //nolint:maintidx
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return "", errors.New("map not found")
	}
	// Get profile ID.
	profileID := ar.Request.URL.Query().Get("profile")

	// Parse destination and prepare options.
	entity := &intel.Entity{}
	destination := ar.URLVars["destination"]
	matchFor := DestinationHub
	var (
		introText              string
		locationV4, locationV6 *geoip.Location
		opts                   *Options
	)
	switch {
	case destination == "":
		// Destination is required.
		return "", errors.New("no destination provided")

	case destination == "home":
		if profileID != "" {
			return "", errors.New("cannot apply profile to home hub route")
		}
		// Simulate finding home hub.
		locations, ok := netenv.GetInternetLocation()
		if !ok || len(locations.All) == 0 {
			return "", errors.New("failed to locate own device for finding home hub")
		}
		introText = fmt.Sprintf("looking for home hub near %s and %s", locations.BestV4(), locations.BestV6())
		locationV4 = locations.BestV4().LocationOrNil()
		locationV6 = locations.BestV6().LocationOrNil()
		matchFor = HomeHub

		// START of copied from captain/navigation.go

		// Get own entity.
		// Checking the entity against the entry policies is somewhat hit and miss
		// anyway, as the device location is an approximation.
		var myEntity *intel.Entity
		if dl := locations.BestV4(); dl != nil && dl.IP != nil {
			myEntity = (&intel.Entity{IP: dl.IP}).Init(0)
			myEntity.FetchData(ar.Context())
		} else if dl := locations.BestV6(); dl != nil && dl.IP != nil {
			myEntity = (&intel.Entity{IP: dl.IP}).Init(0)
			myEntity.FetchData(ar.Context())
		}

		// Build navigation options for searching for a home hub.
		homePolicy, err := endpoints.ParseEndpoints(config.GetAsStringArray("spn/homePolicy", []string{})())
		if err != nil {
			return "", fmt.Errorf("failed to parse home hub policy: %w", err)
		}

		opts = &Options{
			Home: &HomeHubOptions{
				HubPolicies:        []endpoints.Endpoints{homePolicy},
				CheckHubPolicyWith: myEntity,
			},
		}

		// Add requirement to only use Safing nodes when not using community nodes.
		if !config.GetAsBool("spn/useCommunityNodes", true)() {
			opts.Home.RequireVerifiedOwners = []string{"Safing"}
		}

		// Require a trusted home node when the routing profile requires less than two hops.
		routingProfile := GetRoutingProfile(config.GetAsString(profile.CfgOptionRoutingAlgorithmKey, DefaultRoutingProfileID)())
		if routingProfile.MinHops < 2 {
			opts.Home.Regard = opts.Home.Regard.Add(StateTrusted)
		}

		// END of copied

	case net.ParseIP(destination) != nil:
		entity.IP = net.ParseIP(destination)

		fallthrough
	case netutils.IsValidFqdn(destination):
		fallthrough
	case netutils.IsValidFqdn(destination + "."):
		// Resolve domain to IP, if not inherired from a previous case.
		var ignoredIPs int
		if entity.IP == nil {
			entity.Domain = destination

			// Resolve name to IPs.
			ips, err := net.DefaultResolver.LookupIP(ar.Context(), "ip", destination)
			if err != nil {
				return "", fmt.Errorf("failed to lookup IP address of %s: %w", destination, err)
			}
			if len(ips) == 0 {
				return "", fmt.Errorf("failed to lookup IP address of %s: no result", destination)
			}

			// Shuffle IPs.
			if len(ips) >= 2 {
				mr := mrand.New(mrand.NewSource(time.Now().UnixNano())) //nolint:gosec
				mr.Shuffle(len(ips), func(i, j int) {
					ips[i], ips[j] = ips[j], ips[i]
				})
			}

			entity.IP = ips[0]
			ignoredIPs = len(ips) - 1
		}
		entity.Init(0)

		// Get location of IP.
		location, ok := entity.GetLocation(ar.Context())
		if !ok {
			return "", fmt.Errorf("failed to get geoip location for %s: %s", entity.IP, entity.LocationError)
		}
		// Assign location to separate variables.
		if entity.IP.To4() != nil {
			locationV4 = location
		} else {
			locationV6 = location
		}

		// Set intro text.
		if entity.Domain != "" {
			introText = fmt.Sprintf("looking for route to %s at %s\n(ignoring %d additional IPs returned by DNS)", entity.IP, formatLocation(location), ignoredIPs)
		} else {
			introText = fmt.Sprintf("looking for route to %s at %s", entity.IP, formatLocation(location))
		}

		// Get profile.
		if profileID != "" {
			var lp *profile.LayeredProfile
			if profileID == "global" {
				// Create new empty profile for easy access to global settings.
				lp = profile.NewLayeredProfile(profile.New(nil))
			} else {
				// Get local profile by ID.
				localProfile, err := profile.GetLocalProfile(profileID, nil, nil)
				if err != nil {
					return "", fmt.Errorf("failed to get profile: %w", err)
				}
				lp = localProfile.LayeredProfile()
			}
			opts = DeriveTunnelOptions(
				lp,
				entity,
				ar.Request.URL.Query().Has("encrypted"),
			)
		} else {
			opts = m.defaultOptions()
		}

	default:
		return "", errors.New("invalid destination provided")
	}

	// Finalize entity.
	entity.Init(0)

	// Start formatting output.
	lines := []string{
		"Routing simulation: " + introText,
		"Please note that this routing simulation does match the behavior of regular routing to 100%.",
		"",
	}

	// Print options.
	// ==================

	lines = append(lines, "Routing Options:")
	lines = append(lines, "Algorithm: "+opts.RoutingProfile)
	if opts.Home != nil {
		lines = append(lines, "Home Options:")
		lines = append(lines, fmt.Sprintf("  Regard: %s", opts.Home.Regard))
		lines = append(lines, fmt.Sprintf("  Disregard: %s", opts.Home.Disregard))
		lines = append(lines, fmt.Sprintf("  No Default: %v", opts.Home.NoDefaults))
		lines = append(lines, fmt.Sprintf("  Hub Policies: %v", opts.Home.HubPolicies))
		lines = append(lines, fmt.Sprintf("  Require Verified Owners: %v", opts.Home.RequireVerifiedOwners))
	}
	if opts.Transit != nil {
		lines = append(lines, "Transit Options:")
		lines = append(lines, fmt.Sprintf("  Regard: %s", opts.Transit.Regard))
		lines = append(lines, fmt.Sprintf("  Disregard: %s", opts.Transit.Disregard))
		lines = append(lines, fmt.Sprintf("  No Default: %v", opts.Transit.NoDefaults))
		lines = append(lines, fmt.Sprintf("  Hub Policies: %v", opts.Transit.HubPolicies))
		lines = append(lines, fmt.Sprintf("  Require Verified Owners: %v", opts.Transit.RequireVerifiedOwners))
	}
	if opts.Destination != nil {
		lines = append(lines, "Destination Options:")
		lines = append(lines, fmt.Sprintf("  Regard: %s", opts.Destination.Regard))
		lines = append(lines, fmt.Sprintf("  Disregard: %s", opts.Destination.Disregard))
		lines = append(lines, fmt.Sprintf("  No Default: %v", opts.Destination.NoDefaults))
		lines = append(lines, fmt.Sprintf("  Hub Policies: %v", opts.Destination.HubPolicies))
		lines = append(lines, fmt.Sprintf("  Require Verified Owners: %v", opts.Destination.RequireVerifiedOwners))
		if opts.Destination.CheckHubPolicyWith != nil {
			lines = append(lines, "  Check Hub Policy With:")
			if opts.Destination.CheckHubPolicyWith.Domain != "" {
				lines = append(lines, fmt.Sprintf("    Domain: %v", opts.Destination.CheckHubPolicyWith.Domain))
			}
			if opts.Destination.CheckHubPolicyWith.IP != nil {
				lines = append(lines, fmt.Sprintf("    IP: %v", opts.Destination.CheckHubPolicyWith.IP))
			}
			if opts.Destination.CheckHubPolicyWith.Port != 0 {
				lines = append(lines, fmt.Sprintf("    Port: %v", opts.Destination.CheckHubPolicyWith.Port))
			}
		}
	}
	lines = append(lines, "\n")

	// Find nearest hubs.
	// ==================

	// Start operating in map.
	m.RLock()
	defer m.RUnlock()
	// Check if map is populated.
	if m.isEmpty() {
		return "", ErrEmptyMap
	}

	// Find nearest hubs.
	nbPins, err := m.findNearestPins(locationV4, locationV6, opts, matchFor, true)
	if err != nil {
		lines = append(lines, fmt.Sprintf("FAILED to find any suitable exit hub: %s", err))
		return strings.Join(lines, "\n"), nil
		// return "", fmt.Errorf("failed to search for nearby pins: %w", err)
	}

	// Print found exits to table.
	lines = append(lines, "Considered Exits (cheapest 10% are shuffled)")
	buf := bytes.NewBuffer(nil)
	tabWriter := tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "Hub Name\tCost\tLocation\n")
	for _, nbPin := range nbPins.pins {
		fmt.Fprintf(tabWriter,
			"%s\t%.0f\t%s\n",
			nbPin.pin.Hub.Name(),
			nbPin.cost,
			formatMultiLocation(nbPin.pin.LocationV4, nbPin.pin.LocationV6),
		)
	}
	_ = tabWriter.Flush()
	lines = append(lines, buf.String())

	// Print too expensive exits to table.
	lines = append(lines, "Too Expensive Exits:")
	buf = bytes.NewBuffer(nil)
	tabWriter = tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "Hub Name\tCost\tLocation\n")
	for _, nbPin := range nbPins.debug.tooExpensive {
		fmt.Fprintf(tabWriter,
			"%s\t%.0f\t%s\n",
			nbPin.pin.Hub.Name(),
			nbPin.cost,
			formatMultiLocation(nbPin.pin.LocationV4, nbPin.pin.LocationV6),
		)
	}
	_ = tabWriter.Flush()
	lines = append(lines, buf.String())

	// Print disregarded exits to table.
	lines = append(lines, "Disregarded Exits:")
	buf = bytes.NewBuffer(nil)
	tabWriter = tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "Hub Name\tReason\tStates\n")
	for _, nbPin := range nbPins.debug.disregarded {
		fmt.Fprintf(tabWriter,
			"%s\t%s\t%s\n",
			nbPin.pin.Hub.Name(),
			nbPin.reason,
			nbPin.pin.State,
		)
	}
	_ = tabWriter.Flush()
	lines = append(lines, buf.String())

	// Find routes.
	// ============

	// Unless we looked for a home node.
	if destination == "home" {
		return strings.Join(lines, "\n"), nil
	}

	// Find routes.
	routes, err := m.findRoutes(nbPins, opts)
	if err != nil {
		lines = append(lines, fmt.Sprintf("FAILED to find routes: %s", err))
		return strings.Join(lines, "\n"), nil
		// return "", fmt.Errorf("failed to find routes: %w", err)
	}

	// Print found routes to table.
	lines = append(lines, "Considered Routes (cheapest 10% are shuffled)")
	buf = bytes.NewBuffer(nil)
	tabWriter = tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "Cost\tPath\n")
	for _, route := range routes.All {
		fmt.Fprintf(tabWriter,
			"%.0f\t%s\n",
			route.TotalCost,
			formatRoute(route, entity.IP),
		)
	}
	_ = tabWriter.Flush()
	lines = append(lines, buf.String())

	return strings.Join(lines, "\n"), nil
}

func formatLocation(loc *geoip.Location) string {
	return fmt.Sprintf(
		"%s (%s - AS%d %s)",
		loc.Country.Name,
		loc.Country.Code,
		loc.AutonomousSystemNumber,
		loc.AutonomousSystemOrganization,
	)
}

func formatMultiLocation(a, b *geoip.Location) string {
	switch {
	case a != nil:
		return formatLocation(a)
	case b != nil:
		return formatLocation(b)
	default:
		return ""
	}
}

func formatRoute(r *Route, dst net.IP) string {
	s := make([]string, 0, len(r.Path)+1)
	for i, hop := range r.Path {
		if i == 0 {
			s = append(s, hop.pin.Hub.Name())
		} else {
			s = append(s, fmt.Sprintf(">> %.2fc >> %s", hop.Cost, hop.pin.Hub.Name()))
		}
	}
	s = append(s, fmt.Sprintf(">> %.2fc >> %s", r.DstCost, dst))
	return strings.Join(s, " ")
}
