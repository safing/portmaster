package navigator

import (
	"sort"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
)

// Map represent a collection of Pins and their relationship and status.
type Map struct {
	sync.RWMutex
	Name string

	all     map[string]*Pin
	intel   *hub.Intel
	regions []*Region

	home         *Pin
	homeTerminal *docks.CraneTerminal

	measuringEnabled bool
	hubUpdateHook    *database.RegisteredHook

	// analysisLock guards access to all of this map's Pin.analysis,
	// regardedPins and the lastDesegrationAttempt fields.
	analysisLock           sync.Mutex
	regardedPins           []*Pin
	lastDesegrationAttempt time.Time
}

// NewMap returns a new and empty Map.
func NewMap(name string, enableMeasuring bool) *Map {
	m := &Map{
		Name:             name,
		all:              make(map[string]*Pin),
		measuringEnabled: enableMeasuring,
	}
	addMapToAPI(m)

	return m
}

// Close removes the map's integration, taking it "offline".
func (m *Map) Close() {
	removeMapFromAPI(m.Name)
}

// GetPin returns the Pin of the Hub with the given ID.
func (m *Map) GetPin(hubID string) (pin *Pin, ok bool) {
	m.RLock()
	defer m.RUnlock()

	pin, ok = m.all[hubID]
	return
}

// GetHome returns the current home and it's accompanying terminal.
// Both may be nil.
func (m *Map) GetHome() (*Pin, *docks.CraneTerminal) {
	m.RLock()
	defer m.RUnlock()

	return m.home, m.homeTerminal
}

// SetHome sets the given hub as the new home. Optionally, a terminal may be
// supplied to accompany the home hub.
func (m *Map) SetHome(id string, t *docks.CraneTerminal) (ok bool) {
	m.Lock()
	defer m.Unlock()

	// Get pin from map.
	newHome, ok := m.all[id]
	if !ok {
		return false
	}

	// Remove home hub state from all pins.
	for _, pin := range m.all {
		pin.removeStates(StateIsHomeHub)
	}

	// Set pin as home.
	m.home = newHome
	m.homeTerminal = t
	m.home.addStates(StateIsHomeHub)

	// Recalculate reachable.
	err := m.recalculateReachableHubs()
	if err != nil {
		log.Warningf("spn/navigator: failed to recalculate reachable hubs: %s", err)
	}

	m.PushPinChanges()
	return true
}

// GetAvailableCountries returns a map of countries including their information
// where the map has pins suitable for the given type.
func (m *Map) GetAvailableCountries(opts *Options, forType HubType) map[string]*geoip.CountryInfo {
	if opts == nil {
		opts = m.defaultOptions()
	}

	m.RLock()
	defer m.RUnlock()

	matcher := opts.Matcher(forType, m.intel)
	countries := make(map[string]*geoip.CountryInfo)
	for _, pin := range m.all {
		if !matcher(pin) {
			continue
		}
		if pin.LocationV4 != nil && countries[pin.LocationV4.Country.Code] == nil {
			countries[pin.LocationV4.Country.Code] = &pin.LocationV4.Country
		}
		if pin.LocationV6 != nil && countries[pin.LocationV6.Country.Code] == nil {
			countries[pin.LocationV6.Country.Code] = &pin.LocationV6.Country
		}
	}

	return countries
}

// isEmpty returns whether the Map is regarded as empty.
func (m *Map) isEmpty() bool {
	if m.home != nil {
		// When a home hub is set, we also regard a map with only one entry to be
		// empty, as this will be the case for Hubs, which will have their own
		// entry in the Map.
		return len(m.all) <= 1
	}

	return len(m.all) == 0
}

func (m *Map) pinList(lockMap bool) []*Pin {
	if lockMap {
		m.RLock()
		defer m.RUnlock()
	}

	// Copy into slice.
	list := make([]*Pin, 0, len(m.all))
	for _, pin := range m.all {
		list = append(list, pin)
	}

	return list
}

func (m *Map) sortedPins(lockMap bool) []*Pin {
	// Get list.
	list := m.pinList(lockMap)

	// Sort list.
	sort.Sort(sortByPinID(list))
	return list
}
