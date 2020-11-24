package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(showCmd)
	// sub-commands of show are registered using registerComponent
}

var showCmd = &cobra.Command{
	Use: "show",
	PersistentPreRunE: func(*cobra.Command, []string) error {
		// All show sub-commands need the registry but no logging.
		return configureRegistry(false)
	},
	Short: "Show the command to run a Portmaster component yourself",
}

func show(opts *Options, cmdArgs []string) error {
	// get original arguments
	args := getExecArgs(opts, cmdArgs)

	// adapt identifier
	if onWindows {
		opts.Identifier += exeSuffix
	}

	file, err := registry.GetFile(platform(opts.Identifier))
	if err != nil {
		return fmt.Errorf("could not get component: %s", err)
	}

	fmt.Printf("%s %s\n", file.Path(), strings.Join(args, " "))

	return nil
}
