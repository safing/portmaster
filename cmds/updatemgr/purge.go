package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
)

func init() {
	rootCmd.AddCommand(purgeCmd)
}

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Remove old resource versions that are superseded by at least three versions",
	Args:  cobra.ExactArgs(1),
	RunE:  purge,
}

func purge(cmd *cobra.Command, args []string) error {
	log.SetLogLevel(log.TraceLevel)
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
	}
	defer log.Shutdown()

	registry.AddIndex(updater.Index{
		Path:   "stable.json",
		Stable: true,
		Beta:   false,
	})

	registry.AddIndex(updater.Index{
		Path:   "beta.json",
		Stable: false,
		Beta:   true,
	})

	err = registry.LoadIndexes(context.TODO())
	if err != nil {
		return err
	}

	err = scanStorage()
	if err != nil {
		return err
	}

	registry.SelectVersions()
	registry.Purge(3)

	return nil
}
