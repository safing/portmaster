package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/safing/portmaster/service/updates"
)

var bundleSettings = updates.BundleFileSettings{
	Name:            "Portmaster Binaries",
	PrimaryArtifact: "linux_amd64/portmaster-core",
	BaseURL:         "https://updates.safing.io/",
	IgnoreFiles: []string{
		// Indexes, checksums, latest symlinks.
		"*.json",
		"sha256*.txt",
		"latest/**",

		// Signatures.
		"*.sig",
		"**/*.sig",

		// Related, but not required artifacts.
		"**/*.apk",
		"**/*install*",
		"**/spn-hub*",
		"**/jess*",
		"**/hubs*.json",
		"**/*mini*.mmdb.gz",

		// Deprecated artifacts.
		"**/profilemgr*.zip",
		"**/settings*.zip",
		"**/monitor*.zip",
		"**/base*.zip",
		"**/console*.zip",
		"**/portmaster-wintoast*.dll",
		"**/portmaster-snoretoast*.exe",
		"**/portmaster-kext*.dll",
	},
	UnpackFiles: map[string]string{
		"gz":  "**/*.gz",
		"zip": "**/app2/**/portmaster-app*.zip",
	},
}

func main() {
	dir := flag.String("dir", "", "path to the directory that contains the artifacts")

	flag.Parse()
	if *dir == "" {
		fmt.Fprintf(os.Stderr, "-dir parameter is required\n")
		return
	}

	bundle, err := updates.GenerateBundleFromDir(*dir, bundleSettings)
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
