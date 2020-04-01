package endpoints

import (
	"regexp"
	"strings"

	"github.com/safing/portmaster/intel"
)

var (
	countryRegex = regexp.MustCompile(`^[A-Z]{2}$`)
)

// EndpointCountry matches countries.
type EndpointCountry struct {
	EndpointBase

	Country string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointCountry) Matches(entity *intel.Entity) (result EPResult, reason string) {
	if entity.IP == nil {
		return Undeterminable, ""
	}

	country, ok := entity.GetCountry()
	if !ok {
		return Undeterminable, ""
	}
	if country == ep.Country {
		return ep.matchesPPP(entity), "IP is located in " + country
	}
	return NoMatch, ""
}

func (ep *EndpointCountry) String() string {
	return ep.renderPPP(ep.Country)
}

func parseTypeCountry(fields []string) (Endpoint, error) {
	if countryRegex.MatchString(fields[1]) {
		ep := &EndpointCountry{
			Country: strings.ToUpper(fields[1]),
		}
		return ep.parsePPP(ep, fields)
	}

	return nil, nil
}
