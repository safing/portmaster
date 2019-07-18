// +build !windows

package main

import "github.com/spf13/cobra"

func runService(cmd *cobra.Command, opts *Options) {
	run(cmd, opts)
}
