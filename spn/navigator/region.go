package navigator

import (
	"context"
	"math"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/hub"
)

const (
	defaultRegionalMinLanesPerHub  = 0.5
	defaultRegionalMaxLanesOnHub   = 2
	defaultSatelliteMinLanesPerHub = 0.3
	defaultInternalMinLanesOnHub   = 3
	defaultInternalMaxHops         = 3
)

// Region specifies a group of Hubs for optimization purposes.
type Region struct {
	ID           string
	Name         string
	config       *hub.RegionConfig
	memberPolicy endpoints.Endpoints

	pins         []*Pin
	regardedPins []*Pin

	regionalMinLanes      int
	regionalMaxLanesOnHub int
	satelliteMinLanes     int
	internalMinLanesOnHub int
	internalMaxHops       int
}

func (region *Region) getName() string {
	switch {
	case region == nil:
		return "-"
	case region.Name != "":
		return region.Name
	default:
		return region.ID
	}
}

func (m *Map) updateRegions(config []*hub.RegionConfig) {
	// Reset map and pins.
	m.regions = make([]*Region, 0, len(config))
	for _, pin := range m.all {
		pin.region = nil
	}

	// Stop if not regions are defined.
	if len(config) == 0 {
		return
	}

	// Build regions from config.
	for _, regionConfig := range config {
		// Check if region has an ID.
		if regionConfig.ID == "" {
			log.Error("spn/navigator: region is missing ID")
			// Abort adding this region to the map.
			continue
		}

		// Create new region.
		region := &Region{
			ID:     regionConfig.ID,
			Name:   regionConfig.Name,
			config: regionConfig,
		}

		// Parse member policy.
		if len(regionConfig.MemberPolicy) == 0 {
			log.Errorf("spn/navigator: member policy of region %s is missing", region.ID)
			// Abort adding this region to the map.
			continue
		}
		memberPolicy, err := endpoints.ParseEndpoints(regionConfig.MemberPolicy)
		if err != nil {
			log.Errorf("spn/navigator: failed to parse member policy of region %s: %s", region.ID, err)
			// Abort adding this region to the map.
			continue
		}
		region.memberPolicy = memberPolicy

		// Recalculate region properties.
		region.recalculateProperties()

		// Add region to map.
		m.regions = append(m.regions, region)
	}

	// Update region in all Pins.
	for _, pin := range m.all {
		m.updatePinRegion(pin)
	}
}

func (region *Region) addPin(pin *Pin) {
	// Find pin in region.
	for _, regionPin := range region.pins {
		if pin.Hub.ID == regionPin.Hub.ID {
			// Pin is already part of region.
			return
		}
	}

	// Check if pin is already part of this region.
	if pin.region != nil && pin.region.ID == region.ID {
		return
	}

	// Remove pin from previous region.
	if pin.region != nil {
		pin.region.removePin(pin)
	}

	// Add new pin to region.
	region.pins = append(region.pins, pin)
	pin.region = region

	// Recalculate region properties.
	region.recalculateProperties()
}

func (region *Region) removePin(pin *Pin) {
	// Find pin index in region.
	removeIndex := -1
	for index, regionPin := range region.pins {
		if pin.Hub.ID == regionPin.Hub.ID {
			removeIndex = index
			break
		}
	}
	if removeIndex < 0 {
		// Pin is not part of region.
		return
	}

	// Remove pin from region.
	region.pins = append(region.pins[:removeIndex], region.pins[removeIndex+1:]...)

	// Recalculate region properties.
	region.recalculateProperties()
}

func (region *Region) recalculateProperties() {
	// Regional properties.
	region.regionalMinLanes = calculateMinLanes(
		len(region.pins),
		region.config.RegionalMinLanes,
		region.config.RegionalMinLanesPerHub,
		defaultRegionalMinLanesPerHub,
	)
	region.regionalMaxLanesOnHub = region.config.RegionalMaxLanesOnHub
	if region.regionalMaxLanesOnHub <= 0 {
		region.regionalMaxLanesOnHub = defaultRegionalMaxLanesOnHub
	}

	// Satellite properties.
	region.satelliteMinLanes = calculateMinLanes(
		len(region.pins),
		region.config.SatelliteMinLanes,
		region.config.SatelliteMinLanesPerHub,
		defaultSatelliteMinLanesPerHub,
	)

	// Internal properties.
	region.internalMinLanesOnHub = region.config.InternalMinLanesOnHub
	if region.internalMinLanesOnHub <= 0 {
		region.internalMinLanesOnHub = defaultInternalMinLanesOnHub
	}
	region.internalMaxHops = region.config.InternalMaxHops
	if region.internalMaxHops <= 0 {
		region.internalMaxHops = defaultInternalMaxHops
	}
	// Values below 2 do not make any sense for max hops.
	if region.internalMaxHops < 2 {
		region.internalMaxHops = 2
	}
}

func calculateMinLanes(regionHubCount, minLanes int, minLanesPerHub, defaultMinLanesPerHub float64) (minLaneCount int) {
	// Validate hub count.
	if regionHubCount <= 0 {
		// Reset to safe value.
		regionHubCount = 1
	}

	// Set to configured minimum lanes.
	minLaneCount = minLanes

	// Raise to configured minimum lanes per Hub.
	if minLanesPerHub != 0 {
		minLanesFromSize := int(math.Ceil(float64(regionHubCount) * minLanesPerHub))
		if minLanesFromSize > minLaneCount {
			minLaneCount = minLanesFromSize
		}
	}

	// Raise to default minimum lanes per Hub, if still 0.
	if minLaneCount <= 0 {
		minLaneCount = int(math.Ceil(float64(regionHubCount) * defaultMinLanesPerHub))
	}

	return minLaneCount
}

func (m *Map) updatePinRegion(pin *Pin) {
	for _, region := range m.regions {
		// Check if pin matches the region's member policy.
		if pin.EntityV4 != nil {
			result, _ := region.memberPolicy.Match(context.TODO(), pin.EntityV4)
			if result == endpoints.Permitted {
				region.addPin(pin)
				return
			}
		}
		if pin.EntityV6 != nil {
			result, _ := region.memberPolicy.Match(context.TODO(), pin.EntityV6)
			if result == endpoints.Permitted {
				region.addPin(pin)
				return
			}
		}
	}
}
