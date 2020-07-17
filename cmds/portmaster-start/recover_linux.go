package main

import (
	"github.com/safing/portmaster/firewall/interception"
	"github.com/spf13/cobra"
)

var recoverIPTablesCmd = &cobra.Command{
	Use:   "recover-iptables",
	Short: "Removes obsolete IP tables rules in case of an unclean shutdown",
	RunE: func(*cobra.Command, []string) error {
		return interception.DeactivateNfqueueFirewall()
	},
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(recoverIPTablesCmd)
}
