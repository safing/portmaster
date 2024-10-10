package main

import (
	"encoding/json"
	"fmt"

	"github.com/safing/portmaster/service/updates"
	"github.com/spf13/cobra"
)

var (
	bundleSettings = updates.BundleFileSettings{
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

	scanCmd = &cobra.Command{
		Use:   "scan",
		Short: "Scans the contents of the specified directory and creates an index from it.",
		RunE:  scan,
	}

	bundleDir string
)

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().StringVarP(&bundleDir, "dir", "d", "", "directory to create index from (required)")
	_ = scanCmd.MarkFlagRequired("dir")
}

func scan(cmd *cobra.Command, args []string) error {
	bundle, err := updates.GenerateBundleFromDir(bundleDir, bundleSettings)
	if err != nil {
		return err
	}

	bundleStr, err := json.MarshalIndent(&bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	fmt.Printf("%s", bundleStr)
	return nil
}
