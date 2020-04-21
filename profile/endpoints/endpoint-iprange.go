package endpoints

import (
	"net"

	"github.com/safing/portmaster/intel"
)

// EndpointIPRange matches IP ranges.
type EndpointIPRange struct {
	EndpointBase

	Net *net.IPNet
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointIPRange) Matches(entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return Undeterminable, nil
	}
	if ep.Net.Contains(entity.IP) {
		return ep.match(ep, entity, ep.Net.String(), "IP is in")
	}
	return NoMatch, nil
}

func (ep *EndpointIPRange) String() string {
	return ep.renderPPP(ep.Net.String())
}

func parseTypeIPRange(fields []string) (Endpoint, error) {
	_, net, err := net.ParseCIDR(fields[1])
	if err == nil {
		ep := &EndpointIPRange{
			Net: net,
		}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
