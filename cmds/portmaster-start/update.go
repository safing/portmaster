package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/safing/portbase/log"
	"github.com/spf13/cobra"
)

var reset bool

func init() {
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(purgeCmd)

	flags := updateCmd.Flags()
	flags.BoolVar(&reset, "reset", false, "Delete all resources and re-download the basic set")
}

var (
	updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Run a manual update process",
		RunE: func(cmd *cobra.Command, args []string) error {
			return downloadUpdates()
		},
	}

	purgeCmd = &cobra.Command{
		Use:   "purge",
		Short: "Remove old resource versions that are superseded by at least three versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return purge()
		},
	}
)

func indexRequired(cmd *cobra.Command) bool {
	switch cmd {
	case updateCmd,
		purgeCmd:
		return true
	default:
		return false
	}
}

func downloadUpdates() error {
	// mark required updates
	if onWindows {
		registry.MandatoryUpdates = []string{
			platform("core/portmaster-core.exe"),
			platform("kext/portmaster-kext.dll"),
			platform("kext/portmaster-kext.sys"),
			platform("start/portmaster-start.exe"),
			platform("notifier/portmaster-notifier.exe"),
			platform("notifier/portmaster-snoretoast.exe"),
		}
	} else {
		registry.MandatoryUpdates = []string{
			platform("core/portmaster-core"),
			platform("start/portmaster-start"),
			platform("notifier/portmaster-notifier"),
		}
	}

	// add updates that we require on all platforms.
	registry.MandatoryUpdates = append(
		registry.MandatoryUpdates,
		platform("app/portmaster-app.zip"),
		"all/ui/modules/portmaster.zip",
	)

	// Add assets that need unpacking.
	registry.AutoUnpack = []string{
		platform("app/portmaster-app.zip"),
	}

	// logging is configured as a persistent pre-run method inherited from
	// the root command but since we don't use run.Run() we need to start
	// logging ourself.
	log.SetLogLevel(log.TraceLevel)
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
	}
	defer log.Shutdown()

	if reset {
		// Delete storage.
		err = os.RemoveAll(registry.StorageDir().Path)
		if err != nil {
			return fmt.Errorf("failed to reset update dir: %s", err)
		}
		err = registry.StorageDir().Ensure()
		if err != nil {
			return fmt.Errorf("failed to create update dir: %s", err)
		}

		// Reset registry state.
		registry.Reset()
	}

	// Update all indexes.
	err = registry.UpdateIndexes(context.TODO())
	if err != nil {
		return err
	}

	// Download all required updates.
	err = registry.DownloadUpdates(context.TODO())
	if err != nil {
		return err
	}

	// Select versions and unpack the selected.
	registry.SelectVersions()
	err = registry.UnpackResources()
	if err != nil {
		return fmt.Errorf("failed to unpack resources: %s", err)
	}

	return nil
}

func purge() error {
	log.SetLogLevel(log.TraceLevel)

	// logging is configured as a persistent pre-run method inherited from
	// the root command but since we don't use run.Run() we need to start
	// logging ourself.
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
	}
	defer log.Shutdown()

	registry.Purge(3)
	return nil
}

func platform(identifier string) string {
	return fmt.Sprintf("%s_%s/%s", runtime.GOOS, runtime.GOARCH, identifier)
}
