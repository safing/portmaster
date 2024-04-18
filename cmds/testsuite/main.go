package main

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "testsuite",
		Short: "An integration and end-to-end test tool for the SPN",
	}

	verbose bool
)

func runTestCommand(cmdFunc func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Setup
		dbDir, err := os.MkdirTemp("", "spn-testsuite-")
		if err != nil {
			makeReports(cmd, fmt.Errorf("internal test error: failed to setup datbases: %w", err))
			return err
		}
		if err = setupDatabases(dbDir); err != nil {
			makeReports(cmd, fmt.Errorf("internal test error: failed to setup datbases: %w", err))
			return err
		}

		// Run Test
		err = cmdFunc(cmd, args)
		if err != nil {
			log.Printf("test failed: %s", err)
		}

		// Report
		makeReports(cmd, err)

		// Cleanup and return more important error.
		cleanUpErr := os.RemoveAll(dbDir)
		if cleanUpErr != nil {
			// Only log if the test failed, so we can return the more important error
			if err == nil {
				return cleanUpErr
			}
			log.Printf("cleanup failed: %s", err)
		}

		return err
	}
}

func makeReports(cmd *cobra.Command, err error) {
	reportToHealthCheckIfEnabled(cmd, err)
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
