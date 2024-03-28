package hub

import "github.com/safing/portmaster/service/intel/geoip"

// InfoOverride holds data to overide hub info information.
type InfoOverride struct {
	// ContinentCode overrides the continent code of the geoip data.
	ContinentCode string
	// CountryCode overrides the country code of the geoip data.
	CountryCode string
	// Coordinates overrides the geo coordinates code of the geoip data.
	Coordinates *geoip.Coordinates
	// ASN overrides the Autonomous System Number of the geoip data.
	ASN uint
	// ASOrg overrides the Autonomous System Organization of the geoip data.
	ASOrg string
}
