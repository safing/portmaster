package endpoints

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/safing/portmaster/intel"
)

var (
	asnRegex = regexp.MustCompile("^(AS)?[0-9]+$")
)

// EndpointASN matches ASNs.
type EndpointASN struct {
	EndpointBase

	ASN    uint
	Reason string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointASN) Matches(entity *intel.Entity) (result EPResult, reason string) {
	if entity.IP == nil {
		return Undeterminable, ""
	}

	asn, ok := entity.GetASN()
	if !ok {
		return Undeterminable, ""
	}
	if asn == ep.ASN {
		return ep.matchesPPP(entity), ep.Reason
	}
	return NoMatch, ""
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
			ASN:    uint(asn),
			Reason: "IP is part of AS" + strconv.FormatInt(int64(asn), 10),
		}
		return ep.parsePPP(ep, fields)
	}

	return nil, nil
}
