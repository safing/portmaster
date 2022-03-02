package main

import ( //nolint:gci,nolintlint
	"os"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/metrics"
	"github.com/safing/portbase/run"
	"github.com/safing/spn/conf"

	// Include packages here.
	_ "github.com/safing/portbase/modules/subsystems"
	_ "github.com/safing/portmaster/core"
	_ "github.com/safing/portmaster/firewall"
	_ "github.com/safing/portmaster/nameserver"
	_ "github.com/safing/portmaster/ui"
	_ "github.com/safing/spn/captain"
)

func main() {
	// set information
	info.Set("Portmaster", "0.8.5", "AGPLv3", true)

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// enable SPN client mode
	conf.EnableClient(true)

	// start
	os.Exit(run.Run())
}
