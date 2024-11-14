package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/updates"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Force an update of all components.",
	RunE:  update,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func update(cmd *cobra.Command, args []string) error {
	// Finalize config.
	err := svcCfg.Init()
	if err != nil {
		return fmt.Errorf("internal configuration error: %w", err)
	}
	// Force logging to stdout.
	svcCfg.LogToStdout = true

	// Start logging.
	_ = log.Start(svcCfg.LogLevel, svcCfg.LogToStdout, svcCfg.LogDir)
	defer log.Shutdown()

	// Create updaters.
	instance := &updateDummyInstance{}
	binaryUpdateConfig, intelUpdateConfig, err := service.MakeUpdateConfigs(svcCfg)
	if err != nil {
		return fmt.Errorf("init updater config: %w", err)
	}
	binaryUpdates, err := updates.New(instance, "Binary Updater", *binaryUpdateConfig)
	if err != nil {
		return fmt.Errorf("configure binary updates: %w", err)
	}
	intelUpdates, err := updates.New(instance, "Intel Updater", *intelUpdateConfig)
	if err != nil {
		return fmt.Errorf("configure intel updates: %w", err)
	}

	// Force update all.
	binErr := binaryUpdates.ForceUpdate()
	if binErr != nil {
		slog.Error("binary update failed", "err", binErr)
	}
	intelErr := intelUpdates.ForceUpdate()
	if intelErr != nil {
		slog.Error("intel update failed", "err", intelErr)
	}

	// Return error.
	if binErr != nil {
		return fmt.Errorf("binary update failed: %w", binErr)
	}
	if intelErr != nil {
		return fmt.Errorf("intel update failed: %w", intelErr)
	}
	return nil
}

type updateDummyInstance struct{}

func (udi *updateDummyInstance) Restart()                                    {}
func (udi *updateDummyInstance) Shutdown()                                   {}
func (udi *updateDummyInstance) Notifications() *notifications.Notifications { return nil }
