package endpoints

import (
	"net"

	"github.com/safing/portmaster/intel"
)

// EndpointIP matches IPs.
type EndpointIP struct {
	EndpointBase

	IP     net.IP
	Reason string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointIP) Matches(entity *intel.Entity) (result EPResult, reason string) {
	if entity.IP == nil {
		return Undeterminable, ""
	}
	if ep.IP.Equal(entity.IP) {
		return ep.matchesPPP(entity), ep.Reason
	}
	return NoMatch, ""
}

func (ep *EndpointIP) String() string {
	return ep.renderPPP(ep.IP.String())
}

func parseTypeIP(fields []string) (Endpoint, error) {
	ip := net.ParseIP(fields[1])
	if ip != nil {
		ep := &EndpointIP{
			IP:     ip,
			Reason: "IP is " + ip.String(),
		}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
