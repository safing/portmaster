package endpoints

import (
	"strings"

	"github.com/safing/portmaster/intel"
)

// EndpointLists matches endpoint lists.
type EndpointLists struct {
	EndpointBase

	ListSet []string
	Lists   string
	Reason  string
}

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointLists) Matches(entity *intel.Entity) (result EPResult, reason string) {
	entity.LoadLists()

	if entity.MatchLists(ep.ListSet) {
		return ep.matchesPPP(entity), entity.ListBlockReason().String()
	}

	return NoMatch, ""
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
			Reason:  "matched lists " + strings.Join(lists, ","),
		}
		return ep.parsePPP(ep, fields)
	}
	return nil, nil
}
