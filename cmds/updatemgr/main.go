package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/safing/portbase/updater"
	"github.com/safing/portbase/utils"
	"github.com/spf13/cobra"
)

var registry *updater.ResourceRegistry

var rootCmd = &cobra.Command{
	Use:   "updatemgr",
	Short: "A simple tool to assist in the update and release process",
	Args:  cobra.ExactArgs(1),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		registry = &updater.ResourceRegistry{}
		err = registry.Initialize(utils.NewDirStructure(absPath, 0o755))
		if err != nil {
			return err
		}

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

		err = registry.ScanStorage("")
		if err != nil {
			return err
		}

		return nil
	},
	SilenceUsage: true,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
