package main

import (
	"os"
	"path/filepath"

	"github.com/safing/portbase/utils"

	"github.com/spf13/cobra"
)

var (
	updatesStorage *utils.DirStructure
)

var rootCmd = &cobra.Command{
	Use:   "uptool",
	Short: "helper tool for the update process",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]

		absPath, err := filepath.Abs(dir)
		if err != nil {
			return err
		}

		updatesStorage = utils.NewDirStructure(absPath, 0755)
		return nil
	},
	SilenceUsage: true,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
