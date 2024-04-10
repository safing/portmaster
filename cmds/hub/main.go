package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/metrics"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/run"
	_ "github.com/safing/portmaster/service/core/base"
	_ "github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/service/updates/helper"
	_ "github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/conf"
)

func init() {
	flag.BoolVar(&updates.RebootOnRestart, "reboot-on-restart", false, "reboot server on auto-upgrade")
}

func main() {
	info.Set("SPN Hub", "0.7.7", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("hub")

	// Configure updating.
	updates.UserAgent = fmt.Sprintf("SPN Hub (%s %s)", runtime.GOOS, runtime.GOARCH)
	helper.IntelOnly()

	// Configure SPN mode.
	conf.EnablePublicHub(true)
	conf.EnableClient(false)

	// Disable module management, as we want to start all modules.
	modules.DisableModuleManagement()

	// Configure microtask threshold.
	// Scale with CPU/GOMAXPROCS count, but keep a baseline and minimum:
	// CPUs -> MicroTasks
	//    0 ->  8 (increased to minimum)
	//    1 ->  8 (increased to minimum)
	//    2 ->  8
	//    3 -> 10
	//    4 -> 12
	//    8 -> 20
	//   16 -> 36
	//
	// Start with number of GOMAXPROCS.
	microTasksThreshold := runtime.GOMAXPROCS(0) * 2
	// Use at least 4 microtasks based on GOMAXPROCS.
	if microTasksThreshold < 4 {
		microTasksThreshold = 4
	}
	// Add a 4 microtask baseline.
	microTasksThreshold += 4
	// Set threshold.
	modules.SetMaxConcurrentMicroTasks(microTasksThreshold)

	// Start.
	os.Exit(run.Run())
}
