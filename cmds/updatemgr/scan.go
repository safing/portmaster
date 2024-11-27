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
	RunE:  scan,
}

func scan(cmd *cobra.Command, args []string) error {
	// Reset and rescan.
	registry.ResetResources()
	err := registry.ScanStorage("")
	if err != nil {
		return err
	}

	// Export latest versions.
	data, err := json.MarshalIndent(exportSelected(true), "", " ")
	if err != nil {
		return err
	}
	// Print them.
	fmt.Println(string(data))

	return nil
}

func exportSelected(preReleases bool) map[string]string {
	registry.SetUsePreReleases(preReleases)
	registry.SelectVersions()
	export := registry.Export()

	versions := make(map[string]string)
	for _, rv := range export {
		versions[rv.Identifier] = rv.SelectedVersion.VersionNumber
	}
	return versions
}
