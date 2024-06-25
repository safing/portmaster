//nolint:gci,nolintlint
package main

import (
	"flag"
	"fmt"
	"runtime"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"

	// Include packages here.
	_ "github.com/safing/portmaster/service/core"
	_ "github.com/safing/portmaster/service/firewall"
	_ "github.com/safing/portmaster/service/nameserver"
	_ "github.com/safing/portmaster/service/ui"
	_ "github.com/safing/portmaster/spn/captain"
)

func main() {
	flag.Parse()

	// set information
	info.Set("Portmaster", "", "GPLv3")

	// Set default log level.
	log.SetLogLevel(log.WarningLevel)
	log.Start()

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("Portmaster Core (%s %s)", runtime.GOOS, runtime.GOARCH)

	// enable SPN client mode
	conf.EnableClient(true)

	// Prep
	err := base.GlobalPrep()
	if err != nil {
		fmt.Printf("global prep failed: %s\n", err)
		return
	}

	// Create
	instance, err := service.New("2.0.0", &service.ServiceConfig{
		ShutdownFunc: func(exitCode int) {
			fmt.Printf("ExitCode: %d\n", exitCode)
		},
	})
	if err != nil {
		fmt.Printf("error creating an instance: %s\n", err)
		return
	}
	// Start
	err = instance.Group.Start()
	if err != nil {
		fmt.Printf("instance start failed: %s\n", err)
		return
	}
}
