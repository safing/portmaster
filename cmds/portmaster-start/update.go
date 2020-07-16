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

	// ok, now we want logging.
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
	}

	return registry.DownloadUpdates(context.TODO())
}

func platform(identifier string) string {
	return fmt.Sprintf("%s_%s/%s", runtime.GOOS, runtime.GOARCH, identifier)
}
