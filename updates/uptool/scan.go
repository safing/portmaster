package main

import (
	"encoding/json"
	"fmt"

	"github.com/safing/portmaster/updates"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(scanCmd)
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan the current directory and print the result",
	RunE:  scan,
}

func scan(cmd *cobra.Command, args []string) error {

	latest, err := updates.ScanForLatest(".", true)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(latest, "", " ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}
