package endpoints

import "github.com/safing/portmaster/intel"

// EndpointAny matches anything.
type EndpointAny struct {
	EndpointBase
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointAny) Matches(entity *intel.Entity) (EPResult, Reason) {
	return ep.match(ep, entity, "*", "matches")
}

func (ep *EndpointAny) String() string {
	return ep.renderPPP("*")
}

func parseTypeAny(fields []string) (Endpoint, error) {
	if fields[1] == "*" {
		ep := &EndpointAny{}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
