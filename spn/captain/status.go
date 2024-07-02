package captain

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/navigator"
)

// SPNStatus holds SPN status information.
type SPNStatus struct {
	record.Base
	sync.Mutex

	Status             SPNStatusName
	HomeHubID          string
	HomeHubName        string
	ConnectedIP        string
	ConnectedTransport string
	ConnectedCountry   *geoip.CountryInfo
	ConnectedSince     *time.Time
}

// SPNStatusName is a SPN status.
type SPNStatusName string

// SPN Stati.
const (
	StatusFailed     SPNStatusName = "failed"
	StatusDisabled   SPNStatusName = "disabled"
	StatusConnecting SPNStatusName = "connecting"
	StatusConnected  SPNStatusName = "connected"
)

var (
	spnStatus = &SPNStatus{
		Status: StatusDisabled,
	}
	spnStatusPushFunc runtime.PushFunc
)

func registerSPNStatusProvider() (err error) {
	spnStatus.SetKey("runtime:spn/status")
	spnStatus.UpdateMeta()
	spnStatusPushFunc, err = runtime.Register("spn/status", runtime.ProvideRecord(spnStatus))
	return
}

func resetSPNStatus(statusName SPNStatusName, overrideEvenIfConnected bool) {
	// Lock for updating values.
	spnStatus.Lock()
	defer spnStatus.Unlock()

	// Ignore when connected and not overriding
	if !overrideEvenIfConnected && spnStatus.Status == StatusConnected {
		return
	}

	// Reset status.
	spnStatus.Status = statusName
	spnStatus.HomeHubID = ""
	spnStatus.HomeHubName = ""
	spnStatus.ConnectedIP = ""
	spnStatus.ConnectedTransport = ""
	spnStatus.ConnectedCountry = nil
	spnStatus.ConnectedSince = nil

	// Push new status.
	pushSPNStatusUpdate()
}

// pushSPNStatusUpdate pushes an update of spnStatus, which must be locked.
func pushSPNStatusUpdate() {
	spnStatus.UpdateMeta()
	spnStatusPushFunc(spnStatus)
}

// GetSPNStatus returns the current SPN status.
func GetSPNStatus() *SPNStatus {
	spnStatus.Lock()
	defer spnStatus.Unlock()

	return &SPNStatus{
		Status:             spnStatus.Status,
		HomeHubID:          spnStatus.HomeHubID,
		HomeHubName:        spnStatus.HomeHubName,
		ConnectedIP:        spnStatus.ConnectedIP,
		ConnectedTransport: spnStatus.ConnectedTransport,
		ConnectedCountry:   spnStatus.ConnectedCountry,
		ConnectedSince:     spnStatus.ConnectedSince,
	}
}

// AddToDebugInfo adds the SPN status to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	spnStatus.Lock()
	defer spnStatus.Unlock()

	// Check if SPN module is enabled.
	var moduleStatus string
	spnEnabled := config.GetAsBool(CfgOptionEnableSPNKey, false)
	if spnEnabled() {
		moduleStatus = "enabled"
	} else {
		moduleStatus = "disabled"
	}

	// Collect status data.
	lines := make([]string, 0, 20)
	lines = append(lines, fmt.Sprintf("HomeHubID:    %v", spnStatus.HomeHubID))
	lines = append(lines, fmt.Sprintf("HomeHubName:  %v", spnStatus.HomeHubName))
	lines = append(lines, fmt.Sprintf("HomeHubIP:    %v", spnStatus.ConnectedIP))
	lines = append(lines, fmt.Sprintf("Transport:    %v", spnStatus.ConnectedTransport))
	if spnStatus.ConnectedSince != nil {
		lines = append(lines, fmt.Sprintf("Connected:    %v ago", time.Since(*spnStatus.ConnectedSince).Round(time.Minute)))
	}
	lines = append(lines, "---")
	lines = append(lines, fmt.Sprintf("Client:       %v", conf.Client()))
	lines = append(lines, fmt.Sprintf("PublicHub:    %v", conf.PublicHub()))
	lines = append(lines, fmt.Sprintf("HubHasIPv4:   %v", conf.HubHasIPv4()))
	lines = append(lines, fmt.Sprintf("HubHasIPv6:   %v", conf.HubHasIPv6()))

	// Collect status data of map.
	if navigator.Main != nil {
		lines = append(lines, "---")
		mainMapStats := navigator.Main.Stats()
		lines = append(lines, fmt.Sprintf("Map %s:", navigator.Main.Name))
		lines = append(lines, fmt.Sprintf("Active Terminals: %d Hubs", mainMapStats.ActiveTerminals))
		// Collect hub states.
		mapStateSummary := make([]string, 0, len(mainMapStats.States))
		for state, cnt := range mainMapStats.States {
			if cnt > 0 {
				mapStateSummary = append(mapStateSummary, fmt.Sprintf("State %s: %d Hubs", state, cnt))
			}
		}
		sort.Strings(mapStateSummary)
		lines = append(lines, mapStateSummary...)
	}

	// Add all data as section.
	di.AddSection(
		fmt.Sprintf("SPN: %s (module %s)", spnStatus.Status, moduleStatus),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		lines...,
	)
}
