package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/safing/portbase/updater"
)

var (
	releaseCmd = &cobra.Command{
		Use:   "release",
		Short: "Release scans the distribution directory and creates registry indexes and the symlink structure",
		Args:  cobra.ExactArgs(1),
		RunE:  release,
	}
	preReleaseCmd = &cobra.Command{
		Use:   "prerelease",
		Short: "Stage scans the specified directory and loads the indexes - it then creates a staging index with all files newer than the stable and beta indexes",
		Args:  cobra.ExactArgs(1),
		RunE:  release,
	}
	preReleaseFrom   string
	resetPreReleases bool
)

func init() {
	rootCmd.AddCommand(releaseCmd)
	rootCmd.AddCommand(preReleaseCmd)

	preReleaseCmd.Flags().StringVar(&preReleaseFrom, "from", "", "Make a pre-release based on the given channel")
	_ = preReleaseCmd.MarkFlagRequired("from")
	preReleaseCmd.Flags().BoolVar(&resetPreReleases, "reset", false, "Reset pre-release assets")
}

func release(cmd *cobra.Command, args []string) error {
	channel := args[0]

	// Check if we want to reset instead.
	if resetPreReleases {
		return removeFilesFromIndex(getChannelVersions(preReleaseFrom, true))
	}

	// Write new index.
	err := writeIndex(
		channel,
		getChannelVersions(preReleaseFrom, false),
	)
	if err != nil {
		return err
	}

	// Only when doing a release:
	if preReleaseFrom == "" {
		// Create symlinks to latest stable versions.
		if !confirm("\nDo you want to write latest symlinks?") {
			fmt.Println("aborted...")
			return nil
		}
		symlinksDir := registry.StorageDir().ChildDir("latest", 0o755)
		err = registry.CreateSymlinks(symlinksDir)
		if err != nil {
			return err
		}
		fmt.Println("written latest symlinks")
	}

	return nil
}

func writeIndex(channel string, versions map[string]string) error {
	// Export versions and format them.
	versionData, err := json.MarshalIndent(versions, "", " ")
	if err != nil {
		return err
	}

	// Build destination path.
	indexFilePath := filepath.Join(registry.StorageDir().Path, channel+".json")

	// Print preview.
	fmt.Printf("%s (%s):\n", channel, indexFilePath)
	fmt.Println(string(versionData))

	// Ask for confirmation.
	if !confirm("\nDo you want to write this index?") {
		fmt.Println("aborted...")
		return nil
	}

	// Write new index to disk.
	err = ioutil.WriteFile(indexFilePath, versionData, 0o0644) //nolint:gosec // 0644 is intended
	if err != nil {
		return err
	}
	fmt.Printf("written %s\n", indexFilePath)

	return nil
}

func removeFilesFromIndex(versions map[string]string) error {
	// Print preview.
	fmt.Println("To be deleted:")
	for _, filePath := range versions {
		fmt.Println(filePath)
	}

	// Ask for confirmation.
	if !confirm("\nDo you want to delete these files?") {
		fmt.Println("aborted...")
		return nil
	}

	// Delete files.
	for _, filePath := range versions {
		err := os.Remove(filePath)
		if err != nil {
			return err
		}
	}
	fmt.Println("deleted")

	return nil
}

func getChannelVersions(prereleaseFrom string, storagePath bool) map[string]string {
	if prereleaseFrom != "" {
		registry.AddIndex(updater.Index{
			Path:       prereleaseFrom + ".json",
			PreRelease: false,
		})
		err := registry.LoadIndexes(context.Background())
		if err != nil {
			panic(err)
		}
	}

	// Sort all versions.
	registry.SelectVersions()
	export := registry.Export()

	// Go through all versions and save the highest version, if not stable or beta.
	versions := make(map[string]string)
	for _, rv := range export {
		highestVersion := rv.Versions[0]

		// Ignore versions that are in the reference release channel.
		if highestVersion.CurrentRelease {
			continue
		}

		// Add highest version of matching release channel.
		if storagePath {
			versions[rv.Identifier] = rv.GetFile().Path()
		} else {
			versions[rv.Identifier] = highestVersion.VersionNumber
		}
	}

	return versions
}
