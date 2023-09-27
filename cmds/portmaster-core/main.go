//nolint:gci,nolintlint
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/metrics"
	"github.com/safing/portbase/run"
	"github.com/safing/portmaster/updates"
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
	info.Set("Portmaster", "1.4.8", "AGPLv3", true)

	// Set default log level.
	log.SetLogLevel(log.WarningLevel)

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("Portmaster Core (%s %s)", runtime.GOOS, runtime.GOARCH)

	// enable SPN client mode
	conf.EnableClient(true)

	// start
	os.Exit(run.Run())
}
