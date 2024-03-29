package navigator

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/spn/hub"
)

func TestSorting(t *testing.T) {
	t.Parallel()

	list := []*Pin{
		{
			Hub: &hub.Hub{
				ID: "a",
			},
			measurements: &hub.Measurements{
				Latency:        3,
				Capacity:       4,
				CalculatedCost: 5,
			},
			analysis: &AnalysisState{
				SuggestedHopDistance: 3,
			},
		},
		{
			Hub: &hub.Hub{
				ID: "b",
			},
			measurements: &hub.Measurements{
				Latency:        4,
				Capacity:       3,
				CalculatedCost: 1,
			},
			analysis: &AnalysisState{
				SuggestedHopDistance: 2,
			},
		},
		{
			Hub: &hub.Hub{
				ID: "c",
			},
			measurements: &hub.Measurements{
				Latency:        5,
				Capacity:       2,
				CalculatedCost: 2,
			},
			analysis: &AnalysisState{
				SuggestedHopDistance: 4,
			},
		},
		{
			Hub: &hub.Hub{
				ID: "d",
			},
			measurements: &hub.Measurements{
				Latency:        1,
				Capacity:       1,
				CalculatedCost: 3,
			},
			analysis: &AnalysisState{
				SuggestedHopDistance: 4,
			},
		},
		{
			Hub: &hub.Hub{
				ID: "e",
			},
			measurements: &hub.Measurements{
				Latency:        2,
				Capacity:       5,
				CalculatedCost: 4,
			},
			analysis: &AnalysisState{
				SuggestedHopDistance: 4,
			},
		},
	}

	sort.Sort(sortByLowestMeasuredCost(list))
	checkSorting(t, list, "b-c-d-e-a")

	sort.Sort(sortBySuggestedHopDistanceAndLowestMeasuredCost(list))
	checkSorting(t, list, "c-d-e-a-b")

	sort.Sort(sortByLowestMeasuredLatency(list))
	checkSorting(t, list, "d-e-a-b-c")

	sort.Sort(sortByHighestMeasuredCapacity(list))
	checkSorting(t, list, "e-a-b-c-d")

	sort.Sort(sortByPinID(list))
	checkSorting(t, list, "a-b-c-d-e")
}

func checkSorting(t *testing.T, sortedList []*Pin, expectedOrder string) {
	t.Helper()

	// Build list ID string.
	ids := make([]string, 0, len(sortedList))
	for _, pin := range sortedList {
		ids = append(ids, pin.Hub.ID)
	}
	sortedIDs := strings.Join(ids, "-")

	// Check for matching order.
	assert.Equal(t, expectedOrder, sortedIDs, "should match")
}
