package endpoints

import (
	"context"
	"strings"

	"github.com/safing/portmaster/service/intel"
)

// EndpointLists matches endpoint lists.
type EndpointLists struct {
	EndpointBase

	ListSet []string
	Lists   string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointLists) Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason) {
	if entity.MatchLists(ep.ListSet) {
		return ep.match(ep, entity, ep.Lists, "filterlist contains", "filterlist", entity.ListBlockReason())
	}

	return NoMatch, nil
}

func (ep *EndpointLists) String() string {
	return ep.renderPPP(ep.Lists)
}

func parseTypeList(fields []string) (Endpoint, error) {
	if strings.HasPrefix(fields[1], "L:") {
		lists := strings.Split(strings.TrimPrefix(fields[1], "L:"), ",")
		ep := &EndpointLists{
			ListSet: lists,
			Lists:   "L:" + strings.Join(lists, ","),
		}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
