package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/log"
)

func init() {
	rootCmd.AddCommand(purgeCmd)
}

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Remove old resource versions that are superseded by at least three versions",
	RunE:  purge,
}

func purge(cmd *cobra.Command, args []string) error {
	log.SetLogLevel(log.TraceLevel)
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
	}
	defer log.Shutdown()

	registry.SelectVersions()
	registry.Purge(3)

	return nil
}
