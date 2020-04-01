package endpoints

import (
	"net"

	"github.com/safing/portmaster/intel"
)

// EndpointIPRange matches IP ranges.
type EndpointIPRange struct {
	EndpointBase

	Net    *net.IPNet
	Reason string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointIPRange) Matches(entity *intel.Entity) (result EPResult, reason string) {
	if entity.IP == nil {
		return Undeterminable, ""
	}
	if ep.Net.Contains(entity.IP) {
		return ep.matchesPPP(entity), ep.Reason
	}
	return NoMatch, ""
}

func (ep *EndpointIPRange) String() string {
	return ep.renderPPP(ep.Net.String())
}

func parseTypeIPRange(fields []string) (Endpoint, error) {
	_, net, err := net.ParseCIDR(fields[1])
	if err == nil {
		ep := &EndpointIPRange{
			Net:    net,
			Reason: "IP is part of " + net.String(),
		}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
