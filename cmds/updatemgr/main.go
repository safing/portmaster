package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/safing/portmaster/service/updates"
)

var binaryMap = map[string]updates.Artifact{
	"portmaster-core": {
		Platform: "linux_amd64",
	},
	"portmaster-core.exe": {
		Platform: "windows_amd64",
	},
	"portmaster-kext.sys": {
		Platform: "windows_amd64",
	},
}

func main() {
	dir := flag.String("dir", "", "path to the directory that contains the artifacts")
	name := flag.String("name", "", "name of the bundle")
	version := flag.String("version", "", "version of the bundle")

	flag.Parse()
	if *dir == "" {
		fmt.Fprintf(os.Stderr, "-dir parameter is required\n")
		return
	}
	if *name == "" {
		fmt.Fprintf(os.Stderr, "-name parameter is required\n")
		return
	}

	bundle, err := updates.GenerateBundleFromDir(*name, *version, binaryMap, *dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate bundle: %s\n", err)
		return
	}

	bundleStr, err := json.MarshalIndent(&bundle, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal bundle: %s\n", err)
	}

	fmt.Printf("%s", bundleStr)
}
