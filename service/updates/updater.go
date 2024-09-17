package updates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/portmaster/base/log"
)

const (
	defaultFileMode    = os.FileMode(0o0644)
	executableFileMode = os.FileMode(0o0744)
	defaultDirMode     = os.FileMode(0o0755)
)

func applyUpdates(updateIndex UpdateIndex, newBundle Bundle) error {
	// Create purge dir.
	err := os.MkdirAll(updateIndex.PurgeDirectory, defaultDirMode)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Read all files in the current version folder.
	files, err := os.ReadDir(updateIndex.Directory)
	if err != nil {
		return err
	}

	// Move current version files into purge folder.
	for _, file := range files {
		filepath := fmt.Sprintf("%s/%s", updateIndex.Directory, file.Name())
		purgePath := fmt.Sprintf("%s/%s", updateIndex.PurgeDirectory, file.Name())
		err := os.Rename(filepath, purgePath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", filepath, err)
		}
	}

	// Move the new index file
	indexFile := fmt.Sprintf("%s/%s", updateIndex.DownloadDirectory, updateIndex.IndexFile)
	newIndexFile := fmt.Sprintf("%s/%s", updateIndex.Directory, updateIndex.IndexFile)
	err = os.Rename(indexFile, newIndexFile)
	if err != nil {
		return fmt.Errorf("failed to move index file %s: %w", indexFile, err)
	}

	// Move downloaded files to the current version folder.
	for _, artifact := range newBundle.Artifacts {
		fromFilepath := fmt.Sprintf("%s/%s", updateIndex.DownloadDirectory, artifact.Filename)
		toFilepath := fmt.Sprintf("%s/%s", updateIndex.Directory, artifact.Filename)
		err = os.Rename(fromFilepath, toFilepath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", fromFilepath, err)
		}
	}
	return nil
}

func deleteUnfinishedDownloads(rootDir string) error {
	return filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the current file has the specified extension
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".download") {
			log.Warningf("updates: deleting unfinished: %s\n", path)
			err := os.Remove(path)
			if err != nil {
				return fmt.Errorf("failed to delete file %s: %w", path, err)
			}
		}

		return nil
	})
}
