package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(showCmd)
	showCmd.AddCommand(showCore)
	showCmd.AddCommand(showApp)
	showCmd.AddCommand(showNotifier)
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the command to run a Portmaster component yourself",
}

var showCore = &cobra.Command{
	Use:   "core",
	Short: "Show command to run the Portmaster Core",
	RunE: func(cmd *cobra.Command, args []string) error {
		return show(cmd, &Options{
			Identifier: "core/portmaster-core",
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

var showApp = &cobra.Command{
	Use:   "app",
	Short: "Show command to run the Portmaster App",
	RunE: func(cmd *cobra.Command, args []string) error {
		return show(cmd, &Options{
			Identifier: "app/portmaster-app",
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

var showNotifier = &cobra.Command{
	Use:   "notifier",
	Short: "Show command to run the Portmaster Notifier",
	RunE: func(cmd *cobra.Command, args []string) error {
		return show(cmd, &Options{
			Identifier: "notifier/portmaster-notifier",
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

func show(cmd *cobra.Command, opts *Options) error {
	// get original arguments
	var args []string
	if len(os.Args) < 4 {
		return cmd.Help()
	}
	args = os.Args[3:]

	// adapt identifier
	if onWindows {
		opts.Identifier += ".exe"
	}

	file, err := getFile(opts)
	if err != nil {
		return fmt.Errorf("could not get component: %s", err)
	}

	fmt.Printf("%s %s\n", file.Path(), strings.Join(args, " "))

	return nil
}
