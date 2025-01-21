package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/updates"
)

const currentPlatform = runtime.GOOS + "_" + runtime.GOARCH

var (
	downloadCmd = &cobra.Command{
		Use:   "download [index URL] [download dir]",
		Short: "Download all artifacts by an index to a directory",
		RunE:  download,
		Args:  cobra.ExactArgs(2),
	}

	downloadPlatform string
)

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringVarP(&downloadPlatform, "platform", "p", currentPlatform, "Define platform to download artifacts for")
}

func download(cmd *cobra.Command, args []string) error {
	// Args.
	indexURL := args[0]
	targetDir := args[1]

	// Check target dir.
	stat, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("failed to access target dir: %w", err)
	}
	if !stat.IsDir() {
		return errors.New("target is not a directory")
	}

	// Create temporary directories.
	tmpDownload, err := os.MkdirTemp("", "portmaster-updatemgr-download-")
	if err != nil {
		return err
	}
	tmpPurge, err := os.MkdirTemp("", "portmaster-updatemgr-purge-")
	if err != nil {
		return err
	}

	// Create updater.
	u, err := updates.New(nil, "", updates.Config{
		Name:              "Downloader",
		Directory:         targetDir,
		DownloadDirectory: tmpDownload,
		PurgeDirectory:    tmpPurge,
		IndexURLs:         []string{indexURL},
		IndexFile:         "index.json",
		Platform:          downloadPlatform,
	})
	if err != nil {
		return err
	}

	// Start logging.
	err = log.Start(log.InfoLevel.Name(), true, "")
	if err != nil {
		return err
	}

	// Download artifacts.
	err = u.ForceUpdate()

	// Stop logging.
	log.Shutdown()

	// Remove tmp dirs
	os.RemoveAll(tmpDownload)
	os.RemoveAll(tmpPurge)

	return err
}
