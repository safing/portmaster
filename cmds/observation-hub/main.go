package main

import (
	"fmt"
	"runtime"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/service/updates/helper"
	"github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/sluice"
)

func main() {
	info.Set("SPN Observation Hub", "0.7.1", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("observer")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("SPN Observation Hub (%s %s)", runtime.GOOS, runtime.GOARCH)
	helper.IntelOnly()

	// Configure SPN mode.
	conf.EnableClient(true)
	conf.EnablePublicHub(false)
	captain.DisableAccount = true

	// Disable unneeded listeners.
	sluice.EnableListener = false
	api.EnableServer = false

	/// TODO(vladimir) initialize dependency modules

	// Disable module management, as we want to start all modules.
	// module.DisableModuleManagement()
	module, err := New(struct{}{})
	if err != nil {
		fmt.Printf("error creating observer: %s\n", err)
		return
	}
	err = module.Start()
	if err != nil {
		fmt.Printf("failed to start observer: %s\n", err)
		return
	}

	// Start.
	// os.Exit(run.Start())
}
