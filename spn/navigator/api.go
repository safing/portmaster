package navigator

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/awalterschulze/gographviz"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
)

var (
	apiMapsLock sync.Mutex
	apiMaps     = make(map[string]*Map)
)

func addMapToAPI(m *Map) {
	apiMapsLock.Lock()
	defer apiMapsLock.Unlock()

	apiMaps[m.Name] = m
}

func getMapForAPI(name string) (m *Map, ok bool) {
	apiMapsLock.Lock()
	defer apiMapsLock.Unlock()

	m, ok = apiMaps[name]
	return
}

func removeMapFromAPI(name string) {
	apiMapsLock.Lock()
	defer apiMapsLock.Unlock()

	delete(apiMaps, name)
}

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/pins`,
		Read:        api.PermitUser,
		StructFunc:  handleMapPinsRequest,
		Name:        "Get SPN map pins",
		Description: "Returns a list of pins on the map.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/intel/update`,
		Write:       api.PermitSelf,
		ActionFunc:  handleIntelUpdateRequest,
		Name:        "Update map intelligence.",
		Description: "Updates the intel data of the map.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/optimization`,
		Read:        api.PermitUser,
		StructFunc:  handleMapOptimizationRequest,
		Name:        "Get SPN map optimization",
		Description: "Returns the calculated optimization for the map.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/optimization/table`,
		Read:        api.PermitUser,
		DataFunc:    handleMapOptimizationTableRequest,
		Name:        "Get SPN map optimization as a table",
		Description: "Returns the calculated optimization for the map as a table.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/measurements`,
		Read:        api.PermitUser,
		StructFunc:  handleMapMeasurementsRequest,
		Name:        "Get SPN map measurements",
		Description: "Returns the measurements of the map.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/measurements/table`,
		MimeType:    api.MimeTypeText,
		Read:        api.PermitUser,
		DataFunc:    handleMapMeasurementsTableRequest,
		Name:        "Get SPN map measurements as a table",
		Description: "Returns the measurements of the map as a table.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/map/{map:[A-Za-z0-9]{1,255}}/graph{format:\.[a-z]{2,4}}`,
		Read:        api.PermitUser,
		HandlerFunc: handleMapGraphRequest,
		Name:        "Get SPN map graph",
		Description: "Returns a graph of the given SPN map.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "map (in path)",
				Value:       "name of map",
				Description: "Specify the map you want to get the map for. The main map is called `main`.",
			},
			{
				Method:      http.MethodGet,
				Field:       "format (in path)",
				Value:       "file type",
				Description: "Specify the format you want to get the map in. Available values: `dot`, `html`. Please note that the html format is only available in development mode.",
			},
		},
	}); err != nil {
		return err
	}

	// Register API endpoints from other files.
	if err := registerRouteAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

func handleMapPinsRequest(ar *api.Request) (i interface{}, err error) {
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return nil, errors.New("map not found")
	}

	// Export all pins.
	sortedPins := m.sortedPins(true)
	exportedPins := make([]*PinExport, len(sortedPins))
	for key, pin := range sortedPins {
		exportedPins[key] = pin.Export()
	}

	return exportedPins, nil
}

func handleIntelUpdateRequest(ar *api.Request) (msg string, err error) {
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return "", errors.New("map not found")
	}

	// Parse new intel data.
	newIntel, err := hub.ParseIntel(ar.InputData)
	if err != nil {
		return "", fmt.Errorf("failed to parse intel data: %w", err)
	}

	// Apply intel data.
	err = m.UpdateIntel(newIntel, cfgOptionTrustNodeNodes())
	if err != nil {
		return "", fmt.Errorf("failed to apply intel data: %w", err)
	}

	return "successfully applied given intel data", nil
}

func handleMapOptimizationRequest(ar *api.Request) (i interface{}, err error) {
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return nil, errors.New("map not found")
	}

	return m.Optimize(nil)
}

func handleMapOptimizationTableRequest(ar *api.Request) (data []byte, err error) {
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return nil, errors.New("map not found")
	}

	// Get optimization result.
	result, err := m.Optimize(nil)
	if err != nil {
		return nil, err
	}

	// Read lock map, as we access pins.
	m.RLock()
	defer m.RUnlock()

	// Get cranes for additional metadata.
	assignedCranes := docks.GetAllAssignedCranes()

	// Write metadata.
	buf := bytes.NewBuffer(nil)
	buf.WriteString("Optimization:\n")
	fmt.Fprintf(buf, "Purpose: %s\n", result.Purpose)
	if len(result.Approach) == 1 {
		fmt.Fprintf(buf, "Approach: %s\n", result.Approach[0])
	} else if len(result.Approach) > 1 {
		buf.WriteString("Approach:\n")
		for _, approach := range result.Approach {
			fmt.Fprintf(buf, "  - %s\n", approach)
		}
	}
	fmt.Fprintf(buf, "MaxConnect: %d\n", result.MaxConnect)
	fmt.Fprintf(buf, "StopOthers: %v\n", result.StopOthers)

	// Build table of suggested connections.
	buf.WriteString("\nSuggested Connections:\n")
	tabWriter := tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "Hub Name\tReason\tDuplicate\tCountry\tRegion\tLatency\tCapacity\tCost\tGeo Prox.\tHub ID\tLifetime Usage\tPeriod Usage\tProt\tStatus\n")
	for _, suggested := range result.SuggestedConnections {
		var dupe string
		if suggested.Duplicate {
			dupe = "yes"
		} else {
			// Only lock dupes once.
			suggested.pin.measurements.Lock()
			defer suggested.pin.measurements.Unlock()
		}

		// Add row.
		fmt.Fprintf(tabWriter,
			"%s\t%s\t%s\t%s\t%s\t%s\t%.2fMbit/s\t%.2fc\t%.2f%%\t%s",
			suggested.Hub.Info.Name,
			suggested.Reason,
			dupe,
			getPinCountry(suggested.pin),
			suggested.pin.region.getName(),
			suggested.pin.measurements.Latency,
			float64(suggested.pin.measurements.Capacity)/1000000,
			suggested.pin.measurements.CalculatedCost,
			suggested.pin.measurements.GeoProximity,
			suggested.Hub.ID,
		)

		// Add usage stats.
		if crane, ok := assignedCranes[suggested.Hub.ID]; ok {
			addUsageStatsToTable(crane, tabWriter)
		}

		// Add linebreak.
		fmt.Fprint(tabWriter, "\n")
	}
	_ = tabWriter.Flush()

	return buf.Bytes(), nil
}

// addUsageStatsToTable compiles some usage stats of a lane and addes them to the table.
// Table Fields: Lifetime Usage, Period Usage, Prot, Mine.
func addUsageStatsToTable(crane *docks.Crane, tabWriter *tabwriter.Writer) {
	ltIn, ltOut, ltStart, pIn, pOut, pStart := crane.NetState.GetTrafficStats()
	ltDuration := time.Since(ltStart)
	pDuration := time.Since(pStart)

	// Build ownership and stopping info.
	var status string
	isMine := crane.IsMine()
	isStopping := crane.IsStopping()
	stoppingRequested, stoppingRequestedByPeer, markedStoppingAt := crane.NetState.StoppingState()
	if isMine {
		status = "mine"
	}
	if isStopping || stoppingRequested || stoppingRequestedByPeer {
		if isMine {
			status += " - "
		}
		status += "stopping "
		if stoppingRequested {
			status += "<r"
		}
		if isStopping {
			status += "!"
		}
		if stoppingRequestedByPeer {
			status += "r>"
		}
		if isStopping && !markedStoppingAt.IsZero() {
			status += " since " + markedStoppingAt.Truncate(time.Minute).String()
		}
	}

	fmt.Fprintf(tabWriter,
		"\t%.2fGB %.2fMbit/s %.2f%%out since %s\t%.2fGB %.2fMbit/s %.2f%%out since %s\t%s\t%s",
		float64(ltIn+ltOut)/1000000000,
		(float64(ltIn+ltOut)/1000000/ltDuration.Seconds())*8,
		float64(ltOut)/float64(ltIn+ltOut)*100,
		ltDuration.Truncate(time.Second),
		float64(pIn+pOut)/1000000000,
		(float64(pIn+pOut)/1000000/pDuration.Seconds())*8,
		float64(pOut)/float64(pIn+pOut)*100,
		pDuration.Truncate(time.Second),
		crane.Transport().Protocol,
		status,
	)
}

func handleMapMeasurementsRequest(ar *api.Request) (i interface{}, err error) {
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return nil, errors.New("map not found")
	}

	// Get and sort pins.
	list := m.pinList(true)
	sort.Sort(sortByLowestMeasuredCost(list))

	// Copy data and return.
	measurements := make([]*hub.Measurements, 0, len(list))
	for _, pin := range list {
		measurements = append(measurements, pin.measurements.Copy())
	}
	return measurements, nil
}

func handleMapMeasurementsTableRequest(ar *api.Request) (data []byte, err error) {
	// Get map.
	m, ok := getMapForAPI(ar.URLVars["map"])
	if !ok {
		return nil, errors.New("map not found")
	}
	matcher := m.DefaultOptions().Transit.Matcher(m.GetIntel())

	// Get and sort pins.
	list := m.pinList(true)
	sort.Sort(sortByLowestMeasuredCost(list))

	// Get cranes for usage stats.
	assignedCranes := docks.GetAllAssignedCranes()

	// Build table and return.
	buf := bytes.NewBuffer(nil)
	tabWriter := tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "Hub Name\tCountry\tRegion\tLatency\tCapacity\tCost\tGeo Prox.\tHub ID\tLifetime Usage\tPeriod Usage\tProt\tStatus\n")
	for _, pin := range list {
		// Only print regarded Hubs.
		if !matcher(pin) {
			continue
		}

		// Add row.
		pin.measurements.Lock()
		defer pin.measurements.Unlock()
		fmt.Fprintf(tabWriter,
			"%s\t%s\t%s\t%s\t%.2fMbit/s\t%.2fc\t%.2f%%\t%s",
			pin.Hub.Info.Name,
			getPinCountry(pin),
			pin.region.getName(),
			pin.measurements.Latency,
			float64(pin.measurements.Capacity)/1000000,
			pin.measurements.CalculatedCost,
			pin.measurements.GeoProximity,
			pin.Hub.ID,
		)

		// Add usage stats.
		if crane, ok := assignedCranes[pin.Hub.ID]; ok {
			addUsageStatsToTable(crane, tabWriter)
		}

		// Add linebreak.
		fmt.Fprint(tabWriter, "\n")
	}
	_ = tabWriter.Flush()

	return buf.Bytes(), nil
}

func getPinCountry(pin *Pin) string {
	switch {
	case pin.LocationV4 != nil && pin.LocationV4.Country.Code != "":
		return pin.LocationV4.Country.Code
	case pin.LocationV6 != nil && pin.LocationV6.Country.Code != "":
		return pin.LocationV6.Country.Code
	case pin.EntityV4 != nil && pin.EntityV4.Country != "":
		return pin.EntityV4.Country
	case pin.EntityV6 != nil && pin.EntityV6.Country != "":
		return pin.EntityV6.Country
	default:
		return ""
	}
}

func handleMapGraphRequest(w http.ResponseWriter, hr *http.Request) {
	r := api.GetAPIRequest(hr)
	if r == nil {
		http.Error(w, "API request invalid.", http.StatusInternalServerError)
		return
	}

	// Get map.
	m, ok := getMapForAPI(r.URLVars["map"])
	if !ok {
		http.Error(w, "Map not found.", http.StatusNotFound)
		return
	}

	// Check format.
	var format string
	switch r.URLVars["format"] {
	case ".dot":
		format = "dot"
	case ".html":
		format = "html"

		// Check if we are in dev mode.
		if !devMode() {
			http.Error(w, "Graph html formatting (js rendering) is only available in dev mode.", http.StatusPreconditionFailed)
			return
		}
	default:
		http.Error(w, "Unsupported format.", http.StatusBadRequest)
		return
	}

	// Build graph.
	graph := gographviz.NewGraph()
	_ = graph.AddAttr("", "overlap", "scale")
	_ = graph.AddAttr("", "center", "true")
	_ = graph.AddAttr("", "ratio", "fill")
	for _, pin := range m.sortedPins(true) {
		_ = graph.AddNode("", pin.Hub.ID, map[string]string{
			"label":     graphNodeLabel(pin),
			"tooltip":   graphNodeTooltip(pin),
			"color":     graphNodeBorderColor(pin),
			"fillcolor": graphNodeColor(pin),
			"shape":     "circle",
			"style":     "filled",
			"fontsize":  "20",
			"penwidth":  "4",
			"margin":    "0",
		})
		for _, lane := range pin.ConnectedTo {
			if graph.IsNode(lane.Pin.Hub.ID) && pin.State != StateNone {
				// Create attributes.
				edgeOptions := map[string]string{
					"tooltip":  graphEdgeTooltip(pin, lane.Pin, lane),
					"color":    graphEdgeColor(pin, lane.Pin, lane),
					"len":      fmt.Sprintf("%f", lane.Latency.Seconds()*200),
					"penwidth": fmt.Sprintf("%f", math.Sqrt(float64(lane.Capacity)/1000000)*2),
				}
				// Add edge.
				_ = graph.AddEdge(pin.Hub.ID, lane.Pin.Hub.ID, false, edgeOptions)
			}
		}
	}

	var mimeType string
	var responseData []byte
	switch format {
	case "dot":
		mimeType = "text/x-dot"
		responseData = []byte(graph.String())
	case "html":
		mimeType = "text/html"
		responseData = []byte(fmt.Sprintf(
			`<!DOCTYPE html><html><meta charset="utf-8"><body style="margin:0;padding:0;">
<style>#graph svg {height: 99.5vh; width: 99.5vw;}</style>
<div id="graph"></div>
<script src="/assets/vendor/js/hpcc-js-wasm-1.13.0/index.min.js"></script>
<script src="/assets/vendor/js/d3-7.3.0/d3.min.js"></script>
<script src="/assets/vendor/js/d3-graphviz-4.1.0/d3-graphviz.min.js"></script>
<script>
d3.select("#graph").graphviz(useWorker=false).engine("neato").renderDot(%s%s%s);
</script>
</body></html>`,
			"`", graph.String(), "`",
		))
	}

	// Write response.
	w.Header().Set("Content-Type", mimeType+"; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(responseData)))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(responseData)
	if err != nil {
		log.Tracer(r.Context()).Warningf("api: failed to write response: %s", err)
	}
}

func graphNodeLabel(pin *Pin) (s string) {
	var comment string
	switch {
	case pin.State == StateNone:
		comment = "dead"
	case pin.State.Has(StateIsHomeHub):
		comment = "Home"
	case pin.State.HasAnyOf(StateSummaryDisregard):
		comment = "disregarded"
	case !pin.State.Has(StateSummaryRegard):
		comment = "not regarded"
	case pin.State.Has(StateTrusted):
		comment = "trusted"
	}
	if comment != "" {
		comment = fmt.Sprintf("\n(%s)", comment)
	}

	if pin.Hub.Status.Load >= 80 {
		comment += fmt.Sprintf("\nHIGH LOAD: %d", pin.Hub.Status.Load)
	}

	return fmt.Sprintf(
		`"%s%s"`,
		strings.ReplaceAll(pin.Hub.Name(), " ", "\n"),
		comment,
	)
}

func graphNodeTooltip(pin *Pin) string {
	// Gather IP info.
	var v4Info, v6Info string
	if pin.Hub.Info.IPv4 != nil {
		if pin.LocationV4 != nil {
			v4Info = fmt.Sprintf(
				"%s (%s AS%d %s)",
				pin.Hub.Info.IPv4.String(),
				pin.LocationV4.Country.Code,
				pin.LocationV4.AutonomousSystemNumber,
				pin.LocationV4.AutonomousSystemOrganization,
			)
		} else {
			v4Info = pin.Hub.Info.IPv4.String()
		}
	}
	if pin.Hub.Info.IPv6 != nil {
		if pin.LocationV6 != nil {
			v6Info = fmt.Sprintf(
				"%s (%s AS%d %s)",
				pin.Hub.Info.IPv6.String(),
				pin.LocationV6.Country.Code,
				pin.LocationV6.AutonomousSystemNumber,
				pin.LocationV6.AutonomousSystemOrganization,
			)
		} else {
			v6Info = pin.Hub.Info.IPv6.String()
		}
	}

	return fmt.Sprintf(
		`"ID: %s
States: %s
Version: %s
IPv4: %s
IPv6: %s
Load: %d
Cost: %.2f"`,
		pin.Hub.ID,
		pin.State,
		pin.Hub.Status.Version,
		v4Info,
		v6Info,
		pin.Hub.Status.Load,
		pin.Cost,
	)
}

func graphEdgeTooltip(from, to *Pin, lane *Lane) string {
	return fmt.Sprintf(
		`"%s <> %s
Latency: %s
Capacity: %.2f Mbit/s
Cost: %.2f"`,
		from.Hub.Info.Name, to.Hub.Info.Name,
		lane.Latency,
		float64(lane.Capacity)/1000000,
		lane.Cost,
	)
}

// Graphviz colors.
// See https://graphviz.org/doc/info/colors.html
const (
	graphColorWarning          = "orange2"
	graphColorError            = "red2"
	graphColorHomeAndConnected = "steelblue2"
	graphColorDisregard        = "tomato2"
	graphColorNotRegard        = "tan2"
	graphColorTrusted          = "seagreen2"
	graphColorDefaultNode      = "seashell2"
	graphColorDefaultEdge      = "black"
	graphColorNone             = "transparent"
)

func graphNodeColor(pin *Pin) string {
	switch {
	case pin.State == StateNone:
		return graphColorNone
	case pin.Hub.Status.Load >= 95:
		return graphColorError
	case pin.Hub.Status.Load >= 80:
		return graphColorWarning
	case pin.State.Has(StateIsHomeHub):
		return graphColorHomeAndConnected
	case pin.State.HasAnyOf(StateSummaryDisregard):
		return graphColorDisregard
	case !pin.State.Has(StateSummaryRegard):
		return graphColorNotRegard
	case pin.State.Has(StateTrusted):
		return graphColorTrusted
	default:
		return graphColorDefaultNode
	}
}

func graphNodeBorderColor(pin *Pin) string {
	switch {
	case pin.HasActiveTerminal():
		return graphColorHomeAndConnected
	default:
		return graphColorNone
	}
}

func graphEdgeColor(from, to *Pin, lane *Lane) string {
	// Check lane stats.
	if lane.Capacity == 0 || lane.Latency == 0 {
		return graphColorWarning
	}
	// Alert if capacity is under 10Mbit/s or latency is over 100ms.
	if lane.Capacity < 10000000 || lane.Latency > 100*time.Millisecond {
		return graphColorError
	}

	// Check for active edge forward.
	if to.HasActiveTerminal() && len(to.Connection.Route.Path) >= 2 {
		secondLastHopIndex := len(to.Connection.Route.Path) - 2
		if to.Connection.Route.Path[secondLastHopIndex].HubID == from.Hub.ID {
			return graphColorHomeAndConnected
		}
	}
	// Check for active edge backward.
	if from.HasActiveTerminal() && len(from.Connection.Route.Path) >= 2 {
		secondLastHopIndex := len(from.Connection.Route.Path) - 2
		if from.Connection.Route.Path[secondLastHopIndex].HubID == to.Hub.ID {
			return graphColorHomeAndConnected
		}
	}

	// Return default color if edge is not active.
	return graphColorDefaultEdge
}
