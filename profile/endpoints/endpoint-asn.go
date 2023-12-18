package endpoints

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/safing/portmaster/intel"
)

var asnRegex = regexp.MustCompile("^AS[0-9]+$")

// EndpointASN matches ASNs.
type EndpointASN struct {
	EndpointBase

	ASN uint
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointASN) Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return NoMatch, nil
	}

	if !entity.IPScope.IsGlobal() {
		return NoMatch, nil
	}

	asn, ok := entity.GetASN(ctx)
	if !ok {
		asnStr := strconv.Itoa(int(ep.ASN))
		return MatchError, ep.makeReason(ep, asnStr, "ASN data not available to match")
	}

	if asn == ep.ASN {
		asnStr := strconv.Itoa(int(ep.ASN))
		return ep.match(ep, entity, asnStr, "IP is part of AS")
	}

	return NoMatch, nil
}

func (ep *EndpointASN) String() string {
	return ep.renderPPP("AS" + strconv.FormatInt(int64(ep.ASN), 10))
}

func parseTypeASN(fields []string) (Endpoint, error) {
	if asnRegex.MatchString(fields[1]) {
		asn, err := strconv.ParseUint(fields[1][2:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse AS number %s", fields[1])
		}

		ep := &EndpointASN{
			ASN: uint(asn),
		}
		return ep.parsePPP(ep, fields)
	}

	return nil, nil
}
