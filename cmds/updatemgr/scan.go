package main

import (
	"encoding/json"
	"fmt"

	"github.com/safing/portmaster/service/updates"
	"github.com/spf13/cobra"
)

var (
	scanConfig = updates.IndexScanConfig{
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

			// Unsupported platforms.
			"darwin_amd64/**",
			"darwin_arm64/**",

			// Deprecated artifacts.
			"**/portmaster-start*",
			"**/portmaster-app*",
			"**/portmaster-notifier*",
			"**/portmaster-wintoast*.dll",
			"**/portmaster-snoretoast*.exe",
			"**/portmaster-kext*.dll",
			"**/profilemgr*.zip",
			"**/settings*.zip",
			"**/monitor*.zip",
			"**/base*.zip",
			"**/console*.zip",
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

	scanDir string
)

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().StringVarP(&scanDir, "dir", "d", "", "directory to create index from (required)")
	_ = scanCmd.MarkFlagRequired("dir")
}

func scan(cmd *cobra.Command, args []string) error {
	index, err := updates.GenerateIndexFromDir(scanDir, scanConfig)
	if err != nil {
		return err
	}

	indexJson, err := json.MarshalIndent(&index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	fmt.Printf("%s", indexJson)
	return nil
}
