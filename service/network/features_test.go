package network

import (
	"testing"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/netutils"
)

// TestUpdateFeaturesWithoutAccount guards the portmasterpro contract: network
// history and bandwidth visibility never depend on an account, a login or the
// SPN. UpdateFeatures must also be safe on connections without a process.
func TestUpdateFeaturesWithoutAccount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		conn        *Connection
		wantHistory bool
	}{
		{
			name:        "internal connection is not recorded",
			conn:        &Connection{Internal: true, Entity: &intel.Entity{IPScope: netutils.Global}},
			wantHistory: false,
		},
		{
			name:        "localhost connection is not recorded",
			conn:        &Connection{Entity: &intel.Entity{IPScope: netutils.HostLocal}},
			wantHistory: false,
		},
		{
			name:        "global connection without process falls back to disabled history",
			conn:        &Connection{Entity: &intel.Entity{IPScope: netutils.Global}},
			wantHistory: false,
		},
		{
			name:        "missing entity does not panic",
			conn:        &Connection{},
			wantHistory: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.conn.UpdateFeatures()

			if tc.conn.HistoryEnabled != tc.wantHistory {
				t.Errorf("HistoryEnabled = %v, want %v", tc.conn.HistoryEnabled, tc.wantHistory)
			}
			if !tc.conn.BandwidthEnabled {
				t.Error("BandwidthEnabled must always be true: bandwidth visibility is a local feature")
			}
		})
	}
}
