package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(scanCmd)
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan the specified directory and print the result",
	Args:  cobra.ExactArgs(1),
	RunE:  scan,
}

func scan(cmd *cobra.Command, args []string) error {
	// Reset and rescan.
	registry.Reset()
	err := registry.ScanStorage("")
	if err != nil {
		return err
	}

	// Export latest versions.
	data, err := json.MarshalIndent(exportSelected(false), "", " ")
	if err != nil {
		return err
	}
	// Print them.
	fmt.Println(string(data))

	return nil
}

func exportSelected(beta bool) map[string]string {
	registry.SetBeta(beta)
	registry.SelectVersions()
	export := registry.Export()

	versions := make(map[string]string)
	for _, rv := range export {
		versions[rv.Identifier] = rv.SelectedVersion.VersionNumber
	}
	return versions
}
