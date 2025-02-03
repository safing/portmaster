package main

import (
	"github.com/safing/portmaster/cmds/cmdbase"
	"github.com/spf13/cobra"
)

var recoverIPTablesFlag bool

func init() {
	rootCmd.Flags().BoolVar(&recoverIPTablesFlag, "recover-iptables", false, "recovers ip table rules (backward compatibility; use command instead)")
}

func runPlatformSpecifics(cmd *cobra.Command, args []string) {
	switch {
	case printVersion:
		runFlagCmd(cmdbase.Version, cmd, args)
	case recoverIPTablesFlag:
		runFlagCmd(recoverIPTables, cmd, args)
	}
}
