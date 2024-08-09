package navigator

import (
	"context"
	"errors"

	"golang.org/x/exp/slices"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/hub"
)

// UpdateIntel supplies the map with new intel data. The data is not copied, so
// it must not be modified after being supplied. If the map is empty, the
// bootstrap hubs will be added to the map.
func (m *Map) UpdateIntel(update *hub.Intel, trustNodes []string) error {
	// Check if intel data is already parsed.
	if update.Parsed() == nil {
		return errors.New("intel data is not parsed")
	}

	m.Lock()
	defer m.Unlock()

	// Update the map's reference to the intel data.
	m.intel = update

	// Update pins with new intel data.
	for _, pin := range m.all {
		// Add/Update location data from IP addresses.
		pin.updateLocationData()

		// Override Pin Data.
		m.updateInfoOverrides(pin)

		// Update Trust and Advisory Statuses.
		m.updateIntelStatuses(pin, trustNodes)

		// Push changes.
		// TODO: Only set when pin changed.
		pin.pushChanges.Set()
	}

	// Configure the map's regions.
	m.updateRegions(m.intel.Regions)

	// Push pin changes.
	m.PushPinChanges()

	log.Infof("spn/navigator: updated intel on map %s", m.Name)

	// Add bootstrap hubs if map is empty.
	if m.isEmpty() {
		return m.addBootstrapHubs(m.intel.BootstrapHubs)
	}
	return nil
}

// GetIntel returns the map's intel data.
func (m *Map) GetIntel() *hub.Intel {
	m.RLock()
	defer m.RUnlock()

	return m.intel
}

func (m *Map) updateIntelStatuses(pin *Pin, trustNodes []string) {
	// Reset all related states.
	pin.removeStates(StateTrusted | StateUsageDiscouraged | StateUsageAsHomeDiscouraged | StateUsageAsDestinationDiscouraged)

	// Check if Intel data is loaded.
	if m.intel == nil {
		return
	}

	// Check Hub Intel
	hubIntel, ok := m.intel.Hubs[pin.Hub.ID]
	if ok {
		// Apply the verified owner, if any.
		pin.VerifiedOwner = hubIntel.VerifiedOwner

		// Check if Hub is discontinued.
		if hubIntel.Discontinued {
			// Reset state, set offline and return.
			pin.State = StateNone
			pin.addStates(StateOffline)
			return
		}

		// Check if Hub is trusted.
		if hubIntel.Trusted {
			pin.addStates(StateTrusted)
		}
	}

	// Check manual trust status.
	switch {
	case slices.Contains[[]string, string](trustNodes, pin.VerifiedOwner):
		pin.addStates(StateTrusted)
	case slices.Contains[[]string, string](trustNodes, pin.Hub.ID):
		pin.addStates(StateTrusted)
	}

	// Check advisories.
	// Check for UsageDiscouraged.
	checkStatusList(
		pin,
		StateUsageDiscouraged,
		m.intel.AdviseOnlyTrustedHubs,
		m.intel.Parsed().HubAdvisory,
	)
	// Check for UsageAsHomeDiscouraged.
	checkStatusList(
		pin,
		StateUsageAsHomeDiscouraged,
		m.intel.AdviseOnlyTrustedHomeHubs,
		m.intel.Parsed().HomeHubAdvisory,
	)
	// Check for UsageAsDestinationDiscouraged.
	checkStatusList(
		pin,
		StateUsageAsDestinationDiscouraged,
		m.intel.AdviseOnlyTrustedDestinationHubs,
		m.intel.Parsed().DestinationHubAdvisory,
	)
}

func checkStatusList(pin *Pin, state PinState, requireTrusted bool, endpointList endpoints.Endpoints) {
	if requireTrusted && !pin.State.Has(StateTrusted) {
		pin.addStates(state)
		return
	}

	if pin.EntityV4 != nil {
		result, _ := endpointList.Match(context.TODO(), pin.EntityV4)
		if result == endpoints.Denied {
			pin.addStates(state)
			return
		}
	}

	if pin.EntityV6 != nil {
		result, _ := endpointList.Match(context.TODO(), pin.EntityV6)
		if result == endpoints.Denied {
			pin.addStates(state)
		}
	}
}

func (m *Map) updateInfoOverrides(pin *Pin) {
	// Check if Intel data is loaded and if there are any overrides.
	if m.intel == nil {
		return
	}

	// Get overrides for this pin.
	hubIntel, ok := m.intel.Hubs[pin.Hub.ID]
	if !ok || hubIntel.Override == nil {
		return
	}
	overrides := hubIntel.Override

	// Apply overrides
	if overrides.CountryCode != "" {
		if pin.LocationV4 != nil {
			pin.LocationV4.Country = geoip.GetCountryInfo(overrides.CountryCode)
		}
		if pin.EntityV4 != nil {
			pin.EntityV4.Country = overrides.CountryCode
		}
		if pin.LocationV6 != nil {
			pin.LocationV6.Country = geoip.GetCountryInfo(overrides.CountryCode)
		}
		if pin.EntityV6 != nil {
			pin.EntityV6.Country = overrides.CountryCode
		}
	}
	if overrides.Coordinates != nil {
		if pin.LocationV4 != nil {
			pin.LocationV4.Coordinates = *overrides.Coordinates
		}
		if pin.EntityV4 != nil {
			pin.EntityV4.Coordinates = overrides.Coordinates
		}
		if pin.LocationV6 != nil {
			pin.LocationV6.Coordinates = *overrides.Coordinates
		}
		if pin.EntityV6 != nil {
			pin.EntityV6.Coordinates = overrides.Coordinates
		}
	}
	if overrides.ASN != 0 {
		if pin.LocationV4 != nil {
			pin.LocationV4.AutonomousSystemNumber = overrides.ASN
		}
		if pin.EntityV4 != nil {
			pin.EntityV4.ASN = overrides.ASN
		}
		if pin.LocationV6 != nil {
			pin.LocationV6.AutonomousSystemNumber = overrides.ASN
		}
		if pin.EntityV6 != nil {
			pin.EntityV6.ASN = overrides.ASN
		}
	}
	if overrides.ASOrg != "" {
		if pin.LocationV4 != nil {
			pin.LocationV4.AutonomousSystemOrganization = overrides.ASOrg
		}
		if pin.EntityV4 != nil {
			pin.EntityV4.ASOrg = overrides.ASOrg
		}
		if pin.LocationV6 != nil {
			pin.LocationV6.AutonomousSystemOrganization = overrides.ASOrg
		}
		if pin.EntityV6 != nil {
			pin.EntityV6.ASOrg = overrides.ASOrg
		}
	}
}
