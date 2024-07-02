package geoip

import (
	"encoding/binary"
	"net"
	"strings"

	"github.com/umahmood/haversine"

	"github.com/safing/portmaster/base/utils"
)

const (
	earthCircumferenceInKm  = 40100 // earth circumference in km
	defaultLocationAccuracy = 100
)

// Location holds information regarding the geographical and network location of an IP address.
// TODO: We are currently re-using the Continent-Code for the region. Update this and all dependencies.
type Location struct {
	Country                      CountryInfo `maxminddb:"country"`
	Coordinates                  Coordinates `maxminddb:"location"`
	AutonomousSystemNumber       uint        `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string      `maxminddb:"autonomous_system_organization"`
	IsAnycast                    bool        `maxminddb:"is_anycast"`
	IsSatelliteProvider          bool        `maxminddb:"is_satellite_provider"`
	IsAnonymousProxy             bool        `maxminddb:"is_anonymous_proxy"`
}

// Coordinates holds geographic coordinates and their estimated accuracy.
type Coordinates struct {
	AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	Latitude       float64 `maxminddb:"latitude"`
	Longitude      float64 `maxminddb:"longitude"`
}

/*
	Location Estimation

	Distance Value

	- 0: Other side of the Internet.
	- 100: Very near, up to same network / datacenter.

	Weighting Goal

	- Exposure to different networks shall be limited as much as possible.
	- A single network should not see a connection over a large distance.
	- Latency should be low.

	Weighting Intentions

	- Being on the same continent is better than being in the same AS.
	- Being in the same country is better than having low coordinate distance.
	- Coordinate distance is only a tie breaker, as accuracy varies heavily.
	- Same AS with lower coordinate distance beats being on the same continent.

	Weighting Configuration
*/

const (
	weightCountryMatch          = 10
	weightRegionMatch           = 10
	weightRegionalNeighborMatch = 10

	weightASNMatch   = 10
	weightASOrgMatch = 10

	weightCoordinateDistance = 50
)

/*
	About the Accuracy Radius

	- Range: 1-1000
	- Seen values (estimation): 1,5,10,20,50,100,200,500,1000
	- The default seems to be 100.

	Cxamples

	- 1.1.1/24 has 1000: Anycast
	- 8.8.0/19 has 1000: Anycast
	- 8.8.52/22 has 1: City of Westfield

	Conclusion

	- Ignore or penalize high accuracy radius.
*/

// EstimateNetworkProximity aims to calculate the distance between two network locations. Returns a proximity value between 0 (far away) and 100 (nearby).
func (l *Location) EstimateNetworkProximity(to *Location) (proximity float32) {
	switch {
	case l.Country.Code != "" && l.Country.Code == to.Country.Code:
		proximity += weightCountryMatch + weightRegionMatch + weightRegionalNeighborMatch
	case l.Country.Continent.Region != "" && l.Country.Continent.Region == to.Country.Continent.Region:
		proximity += weightRegionMatch + weightRegionalNeighborMatch
	case l.IsRegionalNeighbor(to):
		proximity += weightRegionalNeighborMatch
	}

	switch {
	case l.AutonomousSystemNumber == to.AutonomousSystemNumber &&
		l.AutonomousSystemNumber != 0:
		// Rely more on the ASN data, as it is more accurate than the ASOrg data,
		// especially when combining location data from multiple sources.
		proximity += weightASNMatch + weightASOrgMatch
	case l.AutonomousSystemOrganization == to.AutonomousSystemOrganization &&
		l.AutonomousSystemNumber != 0 && // Check if an ASN is set. If the ASOrg is known, the ASN must be too.
		!ASOrgUnknown(l.AutonomousSystemOrganization): // Check if the ASOrg name is valid.
		proximity += weightASOrgMatch
	}

	// Check coordinates and adjust accuracy value.
	accuracy := l.Coordinates.AccuracyRadius
	switch {
	case l.Coordinates.Latitude == 0 && l.Coordinates.Longitude == 0:
		fallthrough
	case to.Coordinates.Latitude == 0 && to.Coordinates.Longitude == 0:
		// If we don't have any coordinates, return.
		return proximity
	case to.Coordinates.AccuracyRadius > accuracy:
		// If the destination accuracy is worse, use that one.
		accuracy = to.Coordinates.AccuracyRadius
	}

	// Apply the default location accuracy if there is none.
	if accuracy == 0 {
		accuracy = defaultLocationAccuracy
	}

	// Calculate coordinate distance in kilometers.
	fromCoords := haversine.Coord{Lat: l.Coordinates.Latitude, Lon: l.Coordinates.Longitude}
	toCoords := haversine.Coord{Lat: to.Coordinates.Latitude, Lon: to.Coordinates.Longitude}
	_, km := haversine.Distance(fromCoords, toCoords)

	if km <= 100 && accuracy <= 100 {
		// Give the full value for highly accurate coordinates within 100km.
		proximity += weightCoordinateDistance
	} else {
		// Else, take a percentage.
		proximityInPercent := (earthCircumferenceInKm - km) / earthCircumferenceInKm

		// Apply penalty for locations with low accuracy (targeting accuracy radius >100).
		// Take away at most 50% of the weight through inaccuracy.
		accuracyModifier := 1 - float64(accuracy)/2000

		// Add proximiy weight.
		proximity += float32(
			weightCoordinateDistance * // Maxmimum weight for this data point.
				proximityInPercent * // Range: 0-1
				accuracyModifier, // Range: 0.5-1
		)
	}

	return proximity
}

// PrimitiveNetworkProximity calculates the numerical distance between two IP addresses. Returns a proximity value between 0 (far away) and 100 (nearby).
func PrimitiveNetworkProximity(from net.IP, to net.IP, ipVersion uint8) int {
	var diff float64

	switch ipVersion {
	case 4:
		// TODO: use ip.To4() and :4
		a := binary.BigEndian.Uint32(from[12:])
		b := binary.BigEndian.Uint32(to[12:])
		if a > b {
			diff = float64(a - b)
		} else {
			diff = float64(b - a)
		}
	case 6:
		a := binary.BigEndian.Uint64(from[:8])
		b := binary.BigEndian.Uint64(to[:8])
		if a > b {
			diff = float64(a - b)
		} else {
			diff = float64(b - a)
		}
	default:
		return 0
	}

	switch ipVersion {
	case 4:
		diff /= 256
		return int((1 - diff/16777216) * 100)
	case 6:
		return int((1 - diff/18446744073709552000) * 100)
	default:
		return 0
	}
}

var unknownASOrgNames = []string{
	"",           // Expected default for unknown.
	"not routed", // Observed as "Not routed" in data set.
	"unknown",    // Observed as "UNKNOWN" in online data set.
	"nil",        // Programmatic unknown value.
	"null",       // Programmatic unknown value.
	"undef",      // Programmatic unknown value.
	"undefined",  // Programmatic unknown value.
}

// ASOrgUnknown return whether the given AS Org string actually is meant to
// mean that the AS Org is unknown.
func ASOrgUnknown(asOrg string) bool {
	return utils.StringInSlice(
		unknownASOrgNames,
		strings.ToLower(asOrg),
	)
}
