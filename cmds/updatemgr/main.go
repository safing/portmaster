package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/safing/portmaster/service/updates"
)

var binaryMap = map[string]updates.Artifact{
	"geoipv4.mmdb.gz": {
		Filename: "geoipv4.mmdb",
		Unpack:   "gz",
	},
	"geoipv6.mmdb.gz": {
		Filename: "geoipv6.mmdb",
		Unpack:   "gz",
	},
}

var ignoreFiles = map[string]struct{}{
	"bin-index.json":   {},
	"intel-index.json": {},
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

	settings := updates.BundleFileSettings{
		Name:        *name,
		Version:     *version,
		Properties:  binaryMap,
		IgnoreFiles: ignoreFiles,
	}
	bundle, err := updates.GenerateBundleFromDir(*dir, settings)
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
