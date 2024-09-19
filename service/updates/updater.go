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

func switchFolders(updateIndex UpdateIndex, newBundle Bundle) error {
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
		currentFilepath := filepath.Join(updateIndex.Directory, file.Name())
		purgePath := filepath.Join(updateIndex.PurgeDirectory, file.Name())
		err := os.Rename(currentFilepath, purgePath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", currentFilepath, err)
		}
	}

	// Move the new index file
	indexFile := filepath.Join(updateIndex.DownloadDirectory, updateIndex.IndexFile)
	newIndexFile := filepath.Join(updateIndex.Directory, updateIndex.IndexFile)
	err = os.Rename(indexFile, newIndexFile)
	if err != nil {
		return fmt.Errorf("failed to move index file %s: %w", indexFile, err)
	}

	// Move downloaded files to the current version folder.
	for _, artifact := range newBundle.Artifacts {
		fromFilepath := filepath.Join(updateIndex.DownloadDirectory, artifact.Filename)
		toFilepath := filepath.Join(updateIndex.Directory, artifact.Filename)
		err = os.Rename(fromFilepath, toFilepath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", fromFilepath, err)
		}
	}
	return nil
}

func deleteUnfinishedDownloads(rootDir string) error {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// Check if the current file has the download extension
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".download") {
			path := filepath.Join(rootDir, e.Name())
			log.Warningf("updates: deleting unfinished download file: %s\n", path)
			err := os.Remove(path)
			if err != nil {
				log.Errorf("updates: failed to delete unfinished download file %s: %s", path, err)
			}
		}
	}
	return nil
}
