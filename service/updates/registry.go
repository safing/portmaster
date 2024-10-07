package updates

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	semver "github.com/hashicorp/go-version"

	"github.com/safing/portmaster/base/log"
)

const (
	defaultFileMode      = os.FileMode(0o0644)
	executableFileMode   = os.FileMode(0o0744)
	executableUIFileMode = os.FileMode(0o0755)
	defaultDirMode       = os.FileMode(0o0755)
)

type Registry struct {
	bundle   *Bundle
	dir      string
	purgeDir string
	files    map[string]File

	version *semver.Version
}

func CreateRegistry(index UpdateIndex) (Registry, error) {
	registry := Registry{
		dir:      index.Directory,
		purgeDir: index.PurgeDirectory,
		files:    make(map[string]File),
	}
	// Parse bundle
	var err error
	registry.bundle, err = LoadBundle(filepath.Join(index.Directory, index.IndexFile))
	if err != nil {
		return Registry{}, err
	}

	// Parse version
	registry.version, err = semver.NewVersion(registry.bundle.Version)
	if err != nil {
		log.Errorf("updates: failed to parse current version: %s", err)
	}

	// Process files
	for _, artifact := range registry.bundle.Artifacts {
		artifactPath := filepath.Join(registry.dir, artifact.Filename)
		registry.files[artifact.Filename] = File{id: artifact.Filename, path: artifactPath, version: registry.bundle.Version, sha256: artifact.SHA256}
	}
	return registry, nil
}

func (r *Registry) performUpgrade(downloadDir string, indexFile string) error {
	// Make sure provided update is valid
	indexFilepath := filepath.Join(downloadDir, indexFile)
	bundle, err := LoadBundle(indexFilepath)
	if err != nil {
		return fmt.Errorf("invalid update: %w", err)
	}

	err = bundle.Verify(downloadDir)
	if err != nil {
		return fmt.Errorf("invalid update: %w", err)
	}

	// Make sure purge dir is empty.
	_ = os.RemoveAll(r.purgeDir)

	// Create purge dir.
	err = os.MkdirAll(r.purgeDir, defaultDirMode)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Move current version files into purge folder.
	log.Debugf("updates: removing the old version")
	for _, file := range r.files {
		purgePath := filepath.Join(r.purgeDir, file.id)
		err := moveFile(file.path, purgePath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", file.path, err)
		}
	}

	// Move the new index file
	log.Debugf("updates: installing the new version")
	newIndexFile := filepath.Join(r.dir, indexFile)
	err = moveFile(indexFilepath, newIndexFile)
	if err != nil {
		return fmt.Errorf("failed to move index file %s: %w", indexFile, err)
	}

	// Move downloaded files to the current version folder.
	for _, artifact := range bundle.Artifacts {
		fromFilepath := filepath.Join(downloadDir, artifact.Filename)
		toFilepath := filepath.Join(r.dir, artifact.Filename)
		err = moveFile(fromFilepath, toFilepath)
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", fromFilepath, err)
		} else {
			log.Debugf("updates: %s moved", artifact.Filename)
		}
	}

	log.Infof("updates: update complete")

	return nil
}

func moveFile(currentPath, newPath string) error {
	err := os.Rename(currentPath, newPath)
	if err == nil {
		// Moving was successful return
		return nil
	}

	log.Debugf("updates: failed to move '%s' fallback to copy+delete: %s -> %s", err, currentPath, newPath)

	// Failed to move, try copy and delete
	currentFile, err := os.Open(currentPath)
	if err != nil {
		return err
	}
	defer func() { _ = currentFile.Close() }()

	newFile, err := os.Create(newPath)
	if err != nil {
		return err
	}
	defer func() { _ = newFile.Close() }()

	_, err = io.Copy(newFile, currentFile)
	if err != nil {
		return err
	}

	// Make sure file is closed before deletion.
	_ = currentFile.Close()
	currentFile = nil

	err = os.Remove(currentPath)
	if err != nil {
		log.Errorf("updates: failed to delete while moving file: %s", err)
	}

	return nil
}

func (r *Registry) performRecoverableUpgrade(downloadDir string, indexFile string) error {
	upgradeError := r.performUpgrade(downloadDir, indexFile)
	if upgradeError != nil {
		err := r.recover()
		recoverStatus := "(recovery successful)"
		if err != nil {
			recoverStatus = "(recovery failed)"
			log.Errorf("updates: failed to recover: %s", err)
		}

		return fmt.Errorf("upgrade failed: %w %s", upgradeError, recoverStatus)
	}
	return nil
}

func (r *Registry) recover() error {
	files, err := os.ReadDir(r.purgeDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		recoverPath := filepath.Join(r.purgeDir, file.Name())
		currentFilepath := filepath.Join(r.dir, file.Name())
		err := moveFile(recoverPath, currentFilepath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Registry) CleanOldFiles() error {
	err := os.RemoveAll(r.purgeDir)
	if err != nil {
		return fmt.Errorf("failed to delete folder: %w", err)
	}
	return nil
}

type File struct {
	id      string
	path    string
	version string
	sha256  string
}

// Identifier return the id of the file witch is the same as the filename.
func (f *File) Identifier() string {
	return f.id
}

// Path returns the path + filename of the file.
func (f *File) Path() string {
	return f.path
}

// Version returns the version of the file. (currently not filled).
func (f *File) Version() string {
	return f.version
}

// Sha256 returns the sha356 sum of the file.
func (f *File) Sha256() string {
	return f.sha256
}
