package core

import (
	"testing"

	"github.com/safing/portmaster/base/config"
)

// TestSoftwareUpdatesDefaultOff guards the portmasterpro contract that
// upstream binary updates are opt-in: applying them would replace this build
// with stock Portmaster and re-introduce SPN and account features.
func TestSoftwareUpdatesDefaultOff(t *testing.T) {
	t.Parallel()

	err := registerUpdateConfig()
	if err != nil {
		t.Fatalf("registerUpdateConfig: %v", err)
	}

	opt, err := config.GetOption(enableSoftwareUpdatesKey)
	if err != nil {
		t.Fatalf("option %q not registered: %v", enableSoftwareUpdatesKey, err)
	}
	if v, ok := opt.DefaultValue.(bool); !ok || v {
		t.Fatalf("%q default = %v, want false", enableSoftwareUpdatesKey, opt.DefaultValue)
	}

	intel, err := config.GetOption(enableIntelUpdatesKey)
	if err != nil {
		t.Fatalf("option %q not registered: %v", enableIntelUpdatesKey, err)
	}
	if v, ok := intel.DefaultValue.(bool); !ok || !v {
		t.Fatalf("%q default = %v, want true (filter lists and geoip stay updated)", enableIntelUpdatesKey, intel.DefaultValue)
	}
}
