package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

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
	err := scanStorage()
	if err != nil {
		return err
	}

	// export beta
	data, err := json.MarshalIndent(exportSelected(true), "", " ")
	if err != nil {
		return err
	}
	// print
	fmt.Println("beta:")
	fmt.Println(string(data))
	// write index
	err = ioutil.WriteFile(filepath.Join(registry.StorageDir().Dir, "beta.json"), data, 0o644) //nolint:gosec // 0644 is intended
	if err != nil {
		return err
	}

	// export stable
	data, err = json.MarshalIndent(exportSelected(false), "", " ")
	if err != nil {
		return err
	}
	// print
	fmt.Println("\nstable:")
	fmt.Println(string(data))
	// write index
	err = ioutil.WriteFile(filepath.Join(registry.StorageDir().Dir, "stable.json"), data, 0o644) //nolint:gosec // 0644 is intended
	if err != nil {
		return err
	}
	// create symlinks
	err = registry.CreateSymlinks(registry.StorageDir().ChildDir("latest", 0o755))
	if err != nil {
		return err
	}
	fmt.Println("\nstable symlinks created")

	return nil
}
