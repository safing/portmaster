package main

import (
	"github.com/safing/portmaster/firewall/interception"
	"github.com/spf13/cobra"
)

var recoverForceFlag bool
var recoverIPTablesCmd = &cobra.Command{
	Use:   "recover-iptables",
	Short: "Removes obsolete IP tables rules in case of an unclean shutdown",
	RunE: func(*cobra.Command, []string) error {
		return interception.DeactivateNfqueueFirewall(recoverForceFlag)
	},
	SilenceUsage: true,
}

func init() {
	recoverIPTablesCmd.Flags().BoolVarP(&recoverForceFlag, "force", "f", false, "Force removal ignoring errors")
	rootCmd.AddCommand(recoverIPTablesCmd)
}
