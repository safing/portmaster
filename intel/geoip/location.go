package geoip

import (
	"encoding/binary"
	"net"

	"github.com/umahmood/haversine"
)

const (
	earthCircumferenceInKm float64 = 40100 // earth circumference in km
)

// Location holds information regarding the geographical and network location of an IP address.
type Location struct {
	Continent struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"continent"`
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Coordinates                  Coordinates `maxminddb:"location"`
	AutonomousSystemNumber       uint        `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string      `maxminddb:"autonomous_system_organization"`
}

type Coordinates struct {
	AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	Latitude       float64 `maxminddb:"latitude"`
	Longitude      float64 `maxminddb:"longitude"`
}

// EstimateNetworkProximity aims to calculate the distance between two network locations. Returns a proximity value between 0 (far away) and 100 (nearby).
func (l *Location) EstimateNetworkProximity(to *Location) (proximity int) {
	/*
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

		- Continent match: 30
		- Country match: 25
		- ASOrg match: 20
		- ASN match: 15
		- Coordinate distance: 0-10
	*/

	if l.Continent.Code != "" &&
		l.Continent.Code == to.Continent.Code {
		proximity += 30
		if l.Country.ISOCode != "" &&
			l.Country.ISOCode == to.Country.ISOCode {
			proximity += 25
		}
	}

	if l.AutonomousSystemOrganization != "" &&
		l.AutonomousSystemOrganization == to.AutonomousSystemOrganization {
		proximity += 20
		if l.AutonomousSystemNumber != 0 &&
			l.AutonomousSystemNumber == to.AutonomousSystemNumber {
			proximity += 15
		}
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

	// Calculate coordinate distance in kilometers.
	fromCoords := haversine.Coord{Lat: l.Coordinates.Latitude, Lon: l.Coordinates.Longitude}
	toCoords := haversine.Coord{Lat: to.Coordinates.Latitude, Lon: to.Coordinates.Longitude}
	_, km := haversine.Distance(fromCoords, toCoords)

	if km <= 50 && accuracy <= 100 {
		// Give a flat out ten for highly accurate coordinates within 50km.
		proximity += 10
	} else {
		// Else, take a percentage.
		distanceInPercent := (earthCircumferenceInKm - km) * 100 / earthCircumferenceInKm

		// Apply penalty for locations with low accuracy (targeting accuracy radius >100).
		accuracyModifier := 1 - float64(accuracy)/2000
		proximity += int(distanceInPercent * 0.10 * accuracyModifier)
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
