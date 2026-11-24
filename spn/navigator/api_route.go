package navigator

import (
	"bytes"
	"errors"
	"fmt"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/conf"
)

func registerRouteAPIEndpoints() error {
	// example URL:
	// http://127.0.0.1:817/api/v1/spn/map/main/route/to/example.com?profile=global&port=443&protocol=tcp&encrypted=true
	// http://127.0.0.1:817/api/v1/spn/map/main/route/to/1.1.1.1?profile=<app-profile-id>&port=53&protocol=udp&encrypted=false
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
				Field:       "port",
				Value:       "1-65535",
				Description: "Destination port of the simulated connection. Only applied with a profile.",
			},
			{
				Method:      http.MethodGet,
				Field:       "protocol",
				Value:       "tcp|udp",
				Description: "Protocol of the simulated connection. Defaults to tcp if a port is given. Only applied with a profile.",
			},
			{
				Method:      http.MethodGet,
				Field:       "encrypted",
				Value:       "true|false",
				Description: "Whether the simulated connection is regarded as encrypted. Defaults to true for well-known encrypted ports, like 443. Only applied with a profile.",
			},
		},
	}); err != nil {
		return err
	}

	return nil
}

var (
	errMapNotFound      = errors.New("map not found or SPN is disabled")
	errInvalidPort      = errors.New("invalid port: must be a number from 1 to 65535")
	errInvalidProtocol  = errors.New("invalid protocol: must be tcp or udp")
	errInvalidEncrypted = errors.New("invalid encrypted value: must be true or false")
)

// routeSimConn holds the connection details of a simulated connection.
type routeSimConn struct {
	port     uint16
	protocol packet.IPProtocol
	// protocolGiven is true if the protocol was set by the request.
	protocolGiven bool

	encrypted bool
	// encryptedGiven is true if encrypted was set by the request.
	encryptedGiven bool
}

// parseRouteSimConn parses the optional connection details of a route
// simulation request and fills in the defaults that are not given.
func parseRouteSimConn(query url.Values) (*routeSimConn, error) {
	simConn := &routeSimConn{}

	if value := query.Get("port"); value != "" {
		port, err := strconv.ParseUint(value, 10, 16)
		if err != nil || port == 0 {
			return nil, errInvalidPort
		}
		simConn.port = uint16(port)
	}

	switch strings.ToLower(query.Get("protocol")) {
	case "":
		// Rules with a port need a protocol to match. Use TCP, as most
		// connections are TCP.
		if simConn.port != 0 {
			simConn.protocol = packet.TCP
		}
	case "tcp":
		simConn.protocol = packet.TCP
		simConn.protocolGiven = true
	case "udp":
		simConn.protocol = packet.UDP
		simConn.protocolGiven = true
	default:
		return nil, errInvalidProtocol
	}

	if query.Has("encrypted") {
		simConn.encryptedGiven = true
		value := query.Get("encrypted")
		if value == "" {
			// Without a value, the parameter counts as true. This keeps the
			// meaning it had when only its presence was checked.
			simConn.encrypted = true
		} else {
			encrypted, err := strconv.ParseBool(value)
			if err != nil {
				return nil, errInvalidEncrypted
			}
			simConn.encrypted = encrypted
		}
	} else {
		// Derive from the port, as the firewall does for real connections.
		simConn.encrypted = netutils.IsEncryptedPort(simConn.port)
	}

	return simConn, nil
}

func handleRouteCalculationRequest(ar *api.Request) (msg string, err error) { //nolint:maintidx
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return "", errMapNotFound
	}
	// Get profile ID.
	profileID := ar.Request.URL.Query().Get("profile")
	// Get connection details.
	simConn, err := parseRouteSimConn(ar.URL.Query())
	if err != nil {
		return "", err
	}

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
		entity.Protocol = uint8(simConn.protocol)
		entity.Port = simConn.port
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
				simConn.encrypted,
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
		"Please note that this routing simulation does not match the behavior of regular routing to 100%.",
	}
	if conf.PublicHub() {
		lines = append([]string{"Note: not applicable on SPN hubs - a hub is its own home hub, so routes start here.", ""}, lines...)
	}
	// List what is never simulated for connections.
	if destination != "home" {
		lines = append(lines,
			"Not simulated:",
			"  - whether the connection uses the SPN at all: SPN setting of the app, SPN rules, local network destinations",
			"  - an exit hub that is reused or avoided for this destination because of an earlier connection",
			"  - the special handling of Portmaster's own DNS queries",
		)
	}
	lines = append(lines, "")

	// Print inputs.
	// ==================

	home, _ := m.GetHome()
	lines = append(lines, formatRouteSimInputs(destination, profileID, simConn, home)...)
	lines = append(lines, "")

	// Print options.
	// ==================

	lines = append(lines, "Routing Options:")
	lines = append(lines, "Algorithm: "+opts.RoutingProfile)
	if opts.RoutingProfile == RoutingProfileHomeID {
		lines = append(lines, "  NOT simulated: a real connection exits at the home hub (if it has the IP version of the destination); the exits and routes below do not apply.")
	}
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

// formatRouteSimInputs returns lines that describe the inputs of a route
// simulation: whether each was given or defaulted, and how a default changes
// the result.
func formatRouteSimInputs(destination, profileID string, simConn *routeSimConn, home *Pin) []string {
	inputLine := func(label, text string) string {
		return fmt.Sprintf("  %-11s %s", label+":", text)
	}

	homeHub := "none"
	if home != nil {
		homeHub = home.Hub.Name() + " (current)"
	}

	// The home hub search uses the global settings only.
	if destination == "home" {
		lines := []string{
			"Inputs:",
			inputLine("Profile", "not used for the home hub search, the global settings apply"),
			inputLine("Home hub", homeHub),
		}
		if simConn.port != 0 || simConn.protocolGiven || simConn.encryptedGiven {
			lines = append(lines, "  Port, protocol and encrypted are not used for the home hub search.")
		}
		return lines
	}

	// Connection details only have an effect together with a profile.
	var notApplied string
	if profileID == "" {
		notApplied = "NOT applied without a profile"
	}
	withNote := func(text string) string {
		if notApplied == "" {
			return text
		}
		return text + " - " + notApplied
	}

	lines := []string{"Inputs:"}

	// Profile.
	switch profileID {
	case "":
		lines = append(lines, inputLine("Profile", "none (default) - your settings and the exit policies of the hubs are NOT applied; add ?profile=global"))
	case "global":
		lines = append(lines, inputLine("Profile", "global (given) - global settings"))
	default:
		lines = append(lines, inputLine("Profile", profileID+" (given) - settings of this app, with global settings as fallback"))
	}

	// Port.
	switch {
	case simConn.port != 0:
		lines = append(lines, inputLine("Port", withNote(fmt.Sprintf("%d (given)", simConn.port))))
	case notApplied == "":
		lines = append(lines, inputLine("Port", "not set (default) - rules with a port never match; hubs whose exit policy allows only some ports are refused; add ?port=443"))
	default:
		lines = append(lines, inputLine("Port", "not set (default)"))
	}

	// Protocol.
	switch {
	case simConn.protocolGiven:
		lines = append(lines, inputLine("Protocol", withNote(simConn.protocol.String()+" (given)")))
	case simConn.protocol != 0 && notApplied != "":
		lines = append(lines, inputLine("Protocol", withNote(simConn.protocol.String()+" (default for a port)")))
	case simConn.protocol != 0:
		lines = append(lines, inputLine("Protocol", simConn.protocol.String()+" (default for a port) - add ?protocol=udp for UDP"))
	case notApplied == "":
		lines = append(lines, inputLine("Protocol", "not set (default) - rules with a protocol never match; add ?port=<port> (tcp by default) or ?protocol=udp"))
	default:
		lines = append(lines, inputLine("Protocol", "not set (default)"))
	}

	// Encrypted.
	encrypted := "no"
	if simConn.encrypted {
		encrypted = "yes"
	}
	switch {
	case simConn.encryptedGiven:
		encrypted += " (given)"
	case simConn.port != 0:
		encrypted += fmt.Sprintf(" (derived from port %d)", simConn.port)
	default:
		encrypted += " (default)"
	}
	switch {
	case notApplied != "":
		if simConn.encryptedGiven || simConn.port != 0 {
			encrypted = withNote(encrypted)
		}
	case simConn.encryptedGiven:
		if !simConn.encrypted {
			encrypted += " - the exit hub must be trusted"
		}
	case !simConn.encrypted:
		encrypted += " - the exit hub must be trusted; add ?encrypted=true to change"
	default:
		encrypted += " - add ?encrypted=false to require a trusted exit hub"
	}
	lines = append(lines, inputLine("Encrypted", encrypted))

	lines = append(lines, inputLine("Home hub", homeHub))

	return lines
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
