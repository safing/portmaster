package geoip

import (
	"encoding/binary"
	"net"

	"github.com/umahmood/haversine"
)

const (
	earthCircumferenceKm float64 = 40100 // earth circumference in km
)

// Location holds information regarding the geographical and network location of an IP address
type Location struct {
	Continent struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"continent"`
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Coordinates struct {
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
	AutonomousSystemNumber       uint   `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

// About GeoLite2 City accuracy_radius:
//
// range: 1-1000
// seen values (from memory): 1,5,10,20,50,100,200,500,1000
// default seems to be 100
//
// examples:
// 1.1.1/24 has 1000: Anycast
// 8.8.0/19 has 1000: Anycast
// 8.8.52/22 has 1: City of Westfield
//
// Conclusion:
// - Ignore location data completely if accuracy_radius > 500

// EstimateNetworkProximity aims to calculate a distance value between 0 and 100.
func (l *Location) EstimateNetworkProximity(to *Location) (proximity int) {
	// Distance Value:
	// 0: other side of the Internet
	// 100: same network/datacenter

	// Weighting:
	// coordinate distance: 0-50
	// continent match: 10
	// country match: 10
	// AS owner match: 15
	// AS network match: 15
	//
	// We prioritize AS information over country information, as it is more accurate and we expect better privacy if we already are in the destination AS.

	// coordinate distance: 0-50
	fromCoords := haversine.Coord{Lat: l.Coordinates.Latitude, Lon: l.Coordinates.Longitude}
	toCoords := haversine.Coord{Lat: to.Coordinates.Latitude, Lon: to.Coordinates.Longitude}
	_, km := haversine.Distance(fromCoords, toCoords)

	// proximity distance by accuracy
	// get worst accuracy rating
	accuracy := l.Coordinates.AccuracyRadius
	if to.Coordinates.AccuracyRadius > accuracy {
		accuracy = to.Coordinates.AccuracyRadius
	}

	if km <= 10 && accuracy <= 200 {
		proximity += 50
	} else {
		distanceIn50Percent := ((earthCircumferenceKm - km) / earthCircumferenceKm) * 50

		// apply penalty for values high values (targeting >100)
		accuracyModifier := 1 - float64(accuracy)/1000
		proximity += int(distanceIn50Percent * accuracyModifier)
	}

	// continent match: 10
	if l.Continent.Code == to.Continent.Code {
		proximity += 10
		// country match: 10
		if l.Country.ISOCode == to.Country.ISOCode {
			proximity += 10
		}
	}

	// AS owner match: 15
	if l.AutonomousSystemOrganization == to.AutonomousSystemOrganization {
		proximity += 15
		// AS network match: 15
		if l.AutonomousSystemNumber == to.AutonomousSystemNumber {
			proximity += 15
		}
	}

	return

}

func PrimitiveNetworkProximity(from net.IP, to net.IP, ipVersion uint8) int {

	var diff float64

	switch ipVersion {
	case 4:
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
		diff = diff / 256
		return int((1 - diff/16777216) * 100)
	case 6:
		return int((1 - diff/18446744073709552000) * 100)
	default:
		return 0
	}
}
