package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/portmaster/base/log"
)

var ErrNotFound error = errors.New("file not found")

const (
	defaultFileMode    = os.FileMode(0o0644)
	executableFileMode = os.FileMode(0o0744)
	defaultDirMode     = os.FileMode(0o0755)
)

type File struct {
	id   string
	path string
}

func (f *File) Identifier() string {
	return f.id
}

func (f *File) Path() string {
	return f.path
}

func (f *File) Version() string {
	return ""
}

type Registry struct {
	updateIndex UpdateIndex

	bundle       *Bundle
	updateBundle *Bundle

	files map[string]File
}

// New create new Registry.
func New(index UpdateIndex) Registry {
	return Registry{
		updateIndex: index,
		files:       make(map[string]File),
	}
}

// Initialize parses and initializes currently installed bundles.
func (reg *Registry) Initialize() error {
	var err error

	// Parse current installed binary bundle.
	reg.bundle, err = parseBundle(reg.updateIndex.Directory, reg.updateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to parse binary bundle: %w", err)
	}

	// Add bundle artifacts to registry.
	reg.processBundle(reg.bundle)

	return nil
}

func (reg *Registry) processBundle(bundle *Bundle) {
	for _, artifact := range bundle.Artifacts {
		artifactPath := fmt.Sprintf("%s/%s", reg.updateIndex.Directory, artifact.Filename)
		reg.files[artifact.Filename] = File{id: artifact.Filename, path: artifactPath}
	}
}

// GetFile returns the object of a artifact by id.
func (reg *Registry) GetFile(id string) (*File, error) {
	file, ok := reg.files[id]
	if ok {
		return &file, nil
	} else {
		log.Errorf("updates: requested file id not found: %s", id)
		for _, file := range reg.files {
			log.Debugf("File: %s", file)
		}
		return nil, ErrNotFound
	}
}

// CheckForUpdates checks if there is a new binary bundle updates.
func (reg *Registry) CheckForUpdates() (bool, error) {
	err := reg.updateIndex.downloadIndexFile()
	if err != nil {
		return false, err
	}

	reg.updateBundle, err = parseBundle(reg.updateIndex.DownloadDirectory, reg.updateIndex.IndexFile)
	if err != nil {
		return false, err
	}

	// TODO(vladimir): Make a better check.
	if reg.bundle.Version != reg.updateBundle.Version {
		return true, nil
	}

	return false, nil
}

// DownloadUpdates downloads available binary updates.
func (reg *Registry) DownloadUpdates() error {
	if reg.updateBundle == nil {
		//  CheckForBinaryUpdates needs to be called before this.
		return fmt.Errorf("no valid update bundle found")
	}
	_ = deleteUnfinishedDownloads(reg.updateIndex.DownloadDirectory)
	reg.updateBundle.downloadAndVerify(reg.updateIndex.DownloadDirectory)
	return nil
}

// ApplyUpdates removes the current binary folder and replaces it with the downloaded one.
func (reg *Registry) ApplyUpdates() error {
	// Create purge dir.
	err := os.MkdirAll(reg.updateIndex.PurgeDirectory, defaultDirMode)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Read all files in the current version folder.
	files, err := os.ReadDir(reg.updateIndex.Directory)
	if err != nil {
		return err
	}

	// Move current version files into purge folder.
	for _, file := range files {
		filepath := fmt.Sprintf("%s/%s", reg.updateIndex.Directory, file.Name())
		purgePath := fmt.Sprintf("%s/%s", reg.updateIndex.PurgeDirectory, file.Name())
		err := os.Rename(filepath, purgePath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", filepath, err)
		}
	}

	// Move the new index file
	indexFile := fmt.Sprintf("%s/%s", reg.updateIndex.DownloadDirectory, reg.updateIndex.IndexFile)
	newIndexFile := fmt.Sprintf("%s/%s", reg.updateIndex.Directory, reg.updateIndex.IndexFile)
	err = os.Rename(indexFile, newIndexFile)
	if err != nil {
		return fmt.Errorf("failed to move index file %s: %w", indexFile, err)
	}

	// Move downloaded files to the current version folder.
	for _, artifact := range reg.bundle.Artifacts {
		fromFilepath := fmt.Sprintf("%s/%s", reg.updateIndex.DownloadDirectory, artifact.Filename)
		toFilepath := fmt.Sprintf("%s/%s", reg.updateIndex.Directory, artifact.Filename)
		err = os.Rename(fromFilepath, toFilepath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", fromFilepath, err)
		}
	}
	return nil
}

func parseBundle(dir string, indexFile string) (*Bundle, error) {
	filepath := fmt.Sprintf("%s/%s", dir, indexFile)
	// Check if the file exists.
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Read
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// Parse
	var bundle Bundle
	err = json.Unmarshal(content, &bundle)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
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
