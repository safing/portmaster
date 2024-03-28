package endpoints

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/safing/portmaster/service/intel"
)

var countryRegex = regexp.MustCompile(`^[A-Z]{2}$`)

// EndpointCountry matches countries.
type EndpointCountry struct {
	EndpointBase

	CountryCode string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointCountry) Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return NoMatch, nil
	}

	if !entity.IPScope.IsGlobal() {
		return NoMatch, nil
	}

	countryInfo := entity.GetCountryInfo(ctx)
	if countryInfo == nil {
		return MatchError, ep.makeReason(ep, "", "country data not available to match")
	}

	if ep.CountryCode == countryInfo.Code {
		return ep.match(
			ep, entity,
			fmt.Sprintf("%s (%s)", countryInfo.Name, countryInfo.Code),
			"IP is located in",
		)
	}
	return NoMatch, nil
}

func (ep *EndpointCountry) String() string {
	return ep.renderPPP(ep.CountryCode)
}

func parseTypeCountry(fields []string) (Endpoint, error) {
	if countryRegex.MatchString(fields[1]) {
		ep := &EndpointCountry{
			CountryCode: strings.ToUpper(fields[1]),
		}
		return ep.parsePPP(ep, fields)
	}

	return nil, nil
}
