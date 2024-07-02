package geoip

import (
	"testing"

	"github.com/safing/portmaster/base/utils"
)

func TestRegions(t *testing.T) {
	t.Parallel()

	// Check if all neighbors are also linked back.
	for key, region := range regions {
		if key != region.ID {
			t.Errorf("region has different key than ID: %s != %s", key, region.ID)
		}
		for _, neighborID := range region.Neighbors {
			if otherRegion, ok := regions[neighborID]; ok {
				if !utils.StringInSlice(otherRegion.Neighbors, region.ID) {
					t.Errorf("region %s has neighbor %s, but is not linked back", region.ID, neighborID)
				}
			} else {
				t.Errorf("region %s does not exist", neighborID)
			}
		}
	}
}
