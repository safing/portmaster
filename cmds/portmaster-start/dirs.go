package main

import (
	"fmt"
	"log"
	"os"

	"github.com/safing/portmaster/base/utils"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(cleanStructureCmd)
}

var cleanStructureCmd = &cobra.Command{
	Use:   "clean-structure",
	Short: "Create and clean the required directory structure",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureLoggingDir(); err != nil {
			return err
		}
		return cleanAndEnsureExecDir()
	},
}

func cleanAndEnsureExecDir() error {
	execDir := dataRoot.ChildDir("exec", utils.PublicWritePermission)

	// Clean up and remove exec dir.
	err := os.RemoveAll(execDir.Path)
	if err != nil {
		log.Printf("WARNING: failed to fully remove exec dir (%q) for cleaning: %s", execDir.Path, err)
	}

	// Re-create exec dir.
	err = execDir.Ensure()
	if err != nil {
		return fmt.Errorf("failed to initialize exec dir (%q): %w", execDir.Path, err)
	}

	return nil
}
