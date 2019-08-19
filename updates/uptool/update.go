package main

import (
	"encoding/json"
	"io/ioutil"

	"github.com/safing/portmaster/updates"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update scans the current directory and updates the index and symlink structure",
	RunE:  update,
}

func update(cmd *cobra.Command, args []string) error {

	latest, err := updates.ScanForLatest(".", true)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(latest, "", " ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile("stable.json", data, 0755)
	if err != nil {
		return err
	}

	err = updates.CreateSymlinks(updatesStorage.ChildDir("latest", 0755), updatesStorage, latest)
	if err != nil {
		return err
	}

	return nil
}
