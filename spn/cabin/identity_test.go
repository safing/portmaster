package cabin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
)

func TestIdentity(t *testing.T) {
	t.Parallel()

	// Register config options for public hub.
	if err := prepPublicHubConfig(); err != nil {
		t.Fatal(err)
	}

	// Create new identity.
	identityTestKey := "core:spn/public/identity"
	id, err := CreateIdentity(module.Ctx, conf.MainMapName)
	if err != nil {
		t.Fatal(err)
	}
	id.SetKey(identityTestKey)

	// Check values
	// Identity
	assert.NotEmpty(t, id.ID, "id.ID must be set")
	assert.NotEmpty(t, id.Map, "id.Map must be set")
	assert.NotNil(t, id.Signet, "id.Signet must be set")
	assert.NotNil(t, id.infoExportCache, "id.infoExportCache must be set")
	assert.NotNil(t, id.statusExportCache, "id.statusExportCache must be set")
	// Hub
	assert.NotEmpty(t, id.Hub.ID, "hub.ID must be set")
	assert.NotEmpty(t, id.Hub.Map, "hub.Map must be set")
	assert.NotZero(t, id.Hub.FirstSeen, "hub.FirstSeen must be set")
	// Info
	assert.NotEmpty(t, id.Hub.Info.ID, "info.ID must be set")
	assert.NotEqual(t, 0, id.Hub.Info.Timestamp, "info.Timestamp must be set")
	assert.NotEqual(t, "", id.Hub.Info.Name, "info.Name must be set (to hostname)")
	// Status
	assert.NotEqual(t, 0, id.Hub.Status.Timestamp, "status.Timestamp must be set")
	assert.NotEmpty(t, id.Hub.Status.Keys, "status.Keys must be set")

	fmt.Printf("id: %+v\n", id)
	fmt.Printf("id.hub: %+v\n", id.Hub)
	fmt.Printf("id.Hub.Info: %+v\n", id.Hub.Info)
	fmt.Printf("id.Hub.Status: %+v\n", id.Hub.Status)

	// Maintenance is run in creation, so nothing should change now.
	changed, err := id.MaintainAnnouncement(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("unexpected change of announcement")
	}
	changed, err = id.MaintainStatus(nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("unexpected change of status")
	}

	// Change lanes.
	lanes := []*hub.Lane{
		{
			ID:       "A",
			Capacity: 1,
			Latency:  2,
		},
		{
			ID:       "B",
			Capacity: 3,
			Latency:  4,
		},
		{
			ID:       "C",
			Capacity: 5,
			Latency:  6,
		},
	}
	changed, err = id.MaintainStatus(lanes, new(int), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("status should have changed")
	}

	// Change nothing.
	changed, err = id.MaintainStatus(lanes, new(int), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("unexpected change of status")
	}

	// Exporting
	_, err = id.ExportAnnouncement()
	if err != nil {
		t.Fatal(err)
	}
	_, err = id.ExportStatus()
	if err != nil {
		t.Fatal(err)
	}

	// Save to and load from database.
	err = id.Save()
	if err != nil {
		t.Fatal(err)
	}
	id2, changed, err := LoadIdentity(identityTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("unexpected change")
	}

	// Check if they match
	assert.Equal(t, id, id2, "identities should be equal")
}
