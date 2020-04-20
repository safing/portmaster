package endpoints

import (
	"net"

	"github.com/safing/portmaster/intel"
)

// EndpointIP matches IPs.
type EndpointIP struct {
	EndpointBase

	IP net.IP
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointIP) Matches(entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return Undeterminable, nil
	}

	if ep.IP.Equal(entity.IP) {
		return ep.match(ep, entity, ep.IP.String(), "IP matches")
	}
	return NoMatch, nil
}

func (ep *EndpointIP) String() string {
	return ep.renderPPP(ep.IP.String())
}

func parseTypeIP(fields []string) (Endpoint, error) {
	ip := net.ParseIP(fields[1])
	if ip != nil {
		ep := &EndpointIP{
			IP: ip,
		}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
