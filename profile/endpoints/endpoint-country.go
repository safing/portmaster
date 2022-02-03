package endpoints

import (
	"context"
	"regexp"
	"strings"

	"github.com/safing/portmaster/intel"
)

var countryRegex = regexp.MustCompile(`^[A-Z]{2}$`)

// EndpointCountry matches countries.
type EndpointCountry struct {
	EndpointBase

	Country string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointCountry) Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return NoMatch, nil
	}

	if !entity.IPScope.IsGlobal() {
		return NoMatch, nil
	}

	country, ok := entity.GetCountry(ctx)
	if !ok {
		return MatchError, ep.makeReason(ep, country, "country data not available to match")
	}

	if country == ep.Country {
		return ep.match(ep, entity, country, "IP is located in")
	}
	return NoMatch, nil
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
