package endpoints

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/safing/portmaster/service/intel"
)

var (
	continentCodePrefix = "C:"
	continentRegex      = regexp.MustCompile(`^C:[A-Z]{2}$`)
)

// EndpointContinent matches countries.
type EndpointContinent struct {
	EndpointBase

	ContinentCode string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointContinent) Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason) {
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

	if ep.ContinentCode == countryInfo.Continent.Code {
		return ep.match(
			ep, entity,
			fmt.Sprintf("%s (%s)", countryInfo.Continent.Name, countryInfo.Continent.Code),
			"IP is located in",
		)
	}

	return NoMatch, nil
}

func (ep *EndpointContinent) String() string {
	return ep.renderPPP(continentCodePrefix + ep.ContinentCode)
}

func parseTypeContinent(fields []string) (Endpoint, error) {
	if continentRegex.MatchString(fields[1]) {
		ep := &EndpointContinent{
			ContinentCode: strings.TrimPrefix(strings.ToUpper(fields[1]), continentCodePrefix),
		}
		return ep.parsePPP(ep, fields)
	}

	return nil, nil
}
