package docks

import (
	"testing"

	"github.com/safing/portmaster/service/core/pmtesting"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/conf"
)

func TestMain(m *testing.M) {
	runningTests = true
	conf.EnablePublicHub(true) // Make hub config available.
	access.EnableTestMode()    // Register test zone instead of real ones.
	pmtesting.TestMain(m, module)
}
