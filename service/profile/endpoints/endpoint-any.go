package endpoints

import (
	"context"

	"github.com/safing/portmaster/service/intel"
)

// EndpointAny matches anything.
type EndpointAny struct {
	EndpointBase
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointAny) Matches(_ context.Context, entity *intel.Entity) (EPResult, Reason) {
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
