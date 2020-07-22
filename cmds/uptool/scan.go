package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

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

	// export stable
	data, err = json.MarshalIndent(exportSelected(false), "", " ")
	if err != nil {
		return err
	}
	// print
	fmt.Println("\nstable:")
	fmt.Println(string(data))

	return nil
}

func scanStorage() error {
	files, err := ioutil.ReadDir(registry.StorageDir().Path)
	if err != nil {
		return err
	}

	// scan "all" and all "os_platform" dirs
	for _, file := range files {
		if file.IsDir() && (file.Name() == "all" || strings.Contains(file.Name(), "_")) {
			err := registry.ScanStorage(filepath.Join(registry.StorageDir().Path, file.Name()))
			if err != nil {
				return err
			}
		}
	}

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
