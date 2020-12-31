package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	stageReset bool
)

func init() {
	rootCmd.AddCommand(stageCmd)
	stageCmd.Flags().BoolVar(&stageReset, "reset", false, "Reset staging assets")
}

var stageCmd = &cobra.Command{
	Use:   "stage",
	Short: "Stage scans the specified directory and loads the indexes - it then creates a staging index with all files newer than the stable and beta indexes",
	RunE:  stage,
}

func stage(cmd *cobra.Command, args []string) error {
	// Check if we want to reset staging instead.
	if stageReset {
		for _, stagedPath := range exportStaging(true) {
			err := os.Remove(stagedPath)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// Export all staged versions and format them.
	stagingData, err := json.MarshalIndent(exportStaging(false), "", " ")
	if err != nil {
		return err
	}

	// Build destination path.
	stagingIndexFilePath := filepath.Join(registry.StorageDir().Path, "staging.json")

	// Print preview.
	fmt.Printf("staging (%s):\n", stagingIndexFilePath)
	fmt.Println(string(stagingData))

	// Ask for confirmation.
	if !confirm("\nDo you want to write this index?") {
		fmt.Println("aborted...")
		return nil
	}

	// Write new index to disk.
	err = ioutil.WriteFile(stagingIndexFilePath, stagingData, 0o644) //nolint:gosec // 0644 is intended
	if err != nil {
		return err
	}
	fmt.Printf("written %s\n", stagingIndexFilePath)

	return nil
}

func exportStaging(storagePath bool) map[string]string {
	// Sort all versions.
	registry.SetBeta(false)
	registry.SelectVersions()
	export := registry.Export()

	// Go through all versions and save the highest version, if not stable or beta.
	versions := make(map[string]string)
	for _, rv := range export {
		// Get highest version.
		v := rv.Versions[0]

		// Do not take stable or beta releases into account.
		if v.StableRelease || v.BetaRelease {
			continue
		}

		// Add highest version to staging
		if storagePath {
			rv.SelectedVersion = v
			versions[rv.Identifier] = rv.GetFile().Path()
		} else {
			versions[rv.Identifier] = v.VersionNumber
		}
	}

	return versions
}
