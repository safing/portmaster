package main

import "github.com/spf13/cobra"

func init() {
	showCmd.AddCommand(showSnoreToast)
	runCmd.AddCommand(runSnoreToast)
}

var showSnoreToast = &cobra.Command{
	Use:   "notifier-snoretoast",
	Short: "Show command to run the Notifier component SnoreToast",
	RunE: func(cmd *cobra.Command, args []string) error {
		return show(cmd, &Options{
			Identifier: "notifier/portmaster-snoretoast.exe",
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

var runSnoreToast = &cobra.Command{
	Use:   "notifier-snoretoast",
	Short: "Run the Notifier component SnoreToast",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleRun(cmd, &Options{
			Identifier:        "notifier/portmaster-snoretoast.exe",
			AllowDownload:     false,
			AllowHidingWindow: true,
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}
