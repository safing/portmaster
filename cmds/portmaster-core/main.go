//nolint:gci,nolintlint
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/run"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"

	// Include packages here.
	_ "github.com/safing/portmaster/base/modules/subsystems"
	_ "github.com/safing/portmaster/service/core"
	_ "github.com/safing/portmaster/service/firewall"
	_ "github.com/safing/portmaster/service/nameserver"
	_ "github.com/safing/portmaster/service/ui"
	_ "github.com/safing/portmaster/spn/captain"
)

func main() {
	// set information
	info.Set("Portmaster", "", "GPLv3")

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
