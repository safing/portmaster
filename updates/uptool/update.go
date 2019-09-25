package main

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"

	"github.com/safing/portmaster/updates"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update scans the specified directory and updates the index and symlink structure",
	Args:  cobra.ExactArgs(1),
	RunE:  update,
}

func update(cmd *cobra.Command, args []string) error {
	latest, err := updates.ScanForLatest(updatesStorage.Path, true)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(latest, "", " ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(updatesStorage.Path, "stable.json"), data, 0755)
	if err != nil {
		return err
	}

	err = updates.CreateSymlinks(updatesStorage.ChildDir("latest", 0755), updatesStorage, latest)
	if err != nil {
		return err
	}

	return nil
}
