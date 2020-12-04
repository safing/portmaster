package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update scans the specified directory and registry the index and symlink structure",
	Args:  cobra.ExactArgs(1),
	RunE:  update,
}

func update(cmd *cobra.Command, args []string) error {
	// Set stable and beta to latest version.
	updateToLatestVersion(true, true)

	// Export versions.
	betaData, err := json.MarshalIndent(exportSelected(true), "", " ")
	if err != nil {
		return err
	}
	stableData, err := json.MarshalIndent(exportSelected(false), "", " ")
	if err != nil {
		return err
	}

	// Build destination paths.
	betaIndexFilePath := filepath.Join(registry.StorageDir().Path, "beta.json")
	stableIndexFilePath := filepath.Join(registry.StorageDir().Path, "stable.json")

	// Print previews.
	fmt.Printf("beta (%s):\n", betaIndexFilePath)
	fmt.Println(string(betaData))
	fmt.Printf("\nstable: (%s)\n", stableIndexFilePath)
	fmt.Println(string(stableData))

	// Ask for confirmation.
	if !confirm("\nDo you want to write these new indexes (and update latest symlinks)?") {
		fmt.Println("aborted...")
		return nil
	}

	// Write indexes.
	err = ioutil.WriteFile(betaIndexFilePath, betaData, 0o644) //nolint:gosec // 0644 is intended
	if err != nil {
		return err
	}
	fmt.Printf("written %s\n", betaIndexFilePath)

	err = ioutil.WriteFile(stableIndexFilePath, stableData, 0o644) //nolint:gosec // 0644 is intended
	if err != nil {
		return err
	}
	fmt.Printf("written %s\n", stableIndexFilePath)

	// Create symlinks to latest stable versions.
	symlinksDir := registry.StorageDir().ChildDir("latest", 0o755)
	err = registry.CreateSymlinks(symlinksDir)
	if err != nil {
		return err
	}
	fmt.Printf("updated stable symlinks in %s\n", symlinksDir.Path)

	return nil
}

func updateToLatestVersion(stable, beta bool) error {
	for _, resource := range registry.Export() {
		sort.Sort(resource)
		err := resource.AddVersion(resource.Versions[0].VersionNumber, false, stable, beta)
		if err != nil {
			return err
		}
	}
	return nil
}
