package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/safing/portbase/log"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updatesCmd)
}

var updatesCmd = &cobra.Command{
	Use:   "update",
	Short: "Run a manual update process",
	RunE: func(cmd *cobra.Command, args []string) error {
		return downloadUpdates()
	},
}

func downloadUpdates() error {
	// mark required updates
	if onWindows {
		registry.MandatoryUpdates = []string{
			platform("core/portmaster-core.exe"),
			platform("start/portmaster-start.exe"),
			platform("app/portmaster-app.exe"),
			platform("notifier/portmaster-notifier.exe"),
			platform("notifier/portmaster-snoretoast.exe"),
		}
	} else {
		registry.MandatoryUpdates = []string{
			platform("core/portmaster-core"),
			platform("start/portmaster-start"),
			platform("app/portmaster-app"),
			platform("notifier/portmaster-notifier"),
		}
	}

	// add updates that we require on all platforms.
	registry.MandatoryUpdates = append(
		registry.MandatoryUpdates,
		"all/ui/modules/base.zip",
	)

	// logging is configured as a persistent pre-run method inherited from
	// the root command but since we don't use run.Run() we need to start
	// logging ourself.
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
	}

	return registry.DownloadUpdates(context.TODO())
}

func platform(identifier string) string {
	return fmt.Sprintf("%s_%s/%s", runtime.GOOS, runtime.GOARCH, identifier)
}
