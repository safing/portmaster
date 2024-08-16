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
	binaryUpdateIndex UpdateIndex
	intelUpdateIndex  UpdateIndex

	binaryBundle *Bundle
	intelBundle  *Bundle

	binaryUpdateBundle *Bundle
	intelUpdateBundle  *Bundle

	files map[string]File
}

// New create new Registry.
func New(binIndex UpdateIndex, intelIndex UpdateIndex) Registry {
	return Registry{
		binaryUpdateIndex: binIndex,
		intelUpdateIndex:  intelIndex,
		files:             make(map[string]File),
	}
}

// Initialize parses and initializes currently installed bundles.
func (reg *Registry) Initialize() error {
	var err error

	// Parse current installed binary bundle.
	reg.binaryBundle, err = parseBundle(reg.binaryUpdateIndex.Directory, reg.binaryUpdateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to parse binary bundle: %w", err)
	}
	// Parse current installed intel bundle.
	reg.intelBundle, err = parseBundle(reg.intelUpdateIndex.Directory, reg.intelUpdateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to parse intel bundle: %w", err)
	}

	// Add bundle artifacts to registry.
	reg.processBundle(reg.binaryBundle)
	reg.processBundle(reg.intelBundle)

	return nil
}

func (reg *Registry) processBundle(bundle *Bundle) {
	for _, artifact := range bundle.Artifacts {
		artifactPath := fmt.Sprintf("%s/%s", bundle.dir, artifact.Filename)
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
		return nil, ErrNotFound
	}
}

// CheckForBinaryUpdates checks if there is a new binary bundle updates.
func (reg *Registry) CheckForBinaryUpdates() (bool, error) {
	err := reg.binaryUpdateIndex.downloadIndexFile()
	if err != nil {
		return false, err
	}

	reg.binaryUpdateBundle, err = parseBundle(reg.binaryUpdateIndex.DownloadDirectory, reg.binaryUpdateIndex.IndexFile)
	if err != nil {
		return false, fmt.Errorf("failed to parse bundle file: %w", err)
	}

	// TODO(vladimir): Make a better check.
	if reg.binaryBundle.Version != reg.binaryUpdateBundle.Version {
		return true, nil
	}

	return false, nil
}

// DownloadBinaryUpdates downloads available binary updates.
func (reg *Registry) DownloadBinaryUpdates() error {
	if reg.binaryUpdateBundle == nil {
		//  CheckForBinaryUpdates needs to be called before this.
		return fmt.Errorf("no valid update bundle found")
	}
	_ = deleteUnfinishedDownloads(reg.binaryBundle.dir)
	reg.binaryUpdateBundle.downloadAndVerify()
	return nil
}

// CheckForIntelUpdates checks if there is a new intel data bundle updates.
func (reg *Registry) CheckForIntelUpdates() (bool, error) {
	err := reg.intelUpdateIndex.downloadIndexFile()
	if err != nil {
		return false, err
	}

	reg.intelUpdateBundle, err = parseBundle(reg.intelUpdateIndex.DownloadDirectory, reg.intelUpdateIndex.IndexFile)
	if err != nil {
		return false, fmt.Errorf("failed to parse bundle file: %w", err)
	}

	// TODO(vladimir): Make a better check.
	if reg.intelBundle.Version != reg.intelUpdateBundle.Version {
		return true, nil
	}

	return false, nil
}

// DownloadIntelUpdates downloads available intel data updates.
func (reg *Registry) DownloadIntelUpdates() error {
	if reg.intelUpdateBundle == nil {
		//  CheckForIntelUpdates needs to be called before this.
		return fmt.Errorf("no valid update bundle found")
	}
	_ = deleteUnfinishedDownloads(reg.intelBundle.dir)
	reg.intelUpdateBundle.downloadAndVerify()
	return nil
}

// ApplyBinaryUpdates removes the current binary folder and replaces it with the downloaded one.
func (reg *Registry) ApplyBinaryUpdates() error {
	bundle, err := parseBundle(reg.binaryUpdateIndex.DownloadDirectory, reg.binaryUpdateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to parse index file: %w", err)
	}
	err = bundle.Verify()
	if err != nil {
		return fmt.Errorf("binary bundle is not valid: %w", err)
	}

	err = os.RemoveAll(reg.binaryUpdateIndex.Directory)
	if err != nil {
		return fmt.Errorf("failed to remove dir: %w", err)
	}
	err = os.Rename(reg.binaryUpdateIndex.DownloadDirectory, reg.binaryUpdateIndex.Directory)
	if err != nil {
		return fmt.Errorf("failed to move dir: %w", err)
	}
	return nil
}

// ApplyIntelUpdates removes the current intel folder and replaces it with the downloaded one.
func (reg *Registry) ApplyIntelUpdates() error {
	bundle, err := parseBundle(reg.intelUpdateIndex.DownloadDirectory, reg.intelUpdateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to parse index file: %w", err)
	}
	err = bundle.Verify()
	if err != nil {
		return fmt.Errorf("binary bundle is not valid: %w", err)
	}

	err = os.RemoveAll(reg.intelUpdateIndex.Directory)
	if err != nil {
		return fmt.Errorf("failed to remove dir: %w", err)
	}
	err = os.Rename(reg.intelUpdateIndex.DownloadDirectory, reg.intelUpdateIndex.Directory)
	if err != nil {
		return fmt.Errorf("failed to move dir: %w", err)
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
	bundle.dir = dir

	return &bundle, nil
}

func deleteUnfinishedDownloads(rootDir string) error {
	return filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the current file has the specified extension
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".download") {
			log.Warningf("updates deleting unfinished: %s\n", path)
			err := os.Remove(path)
			if err != nil {
				return fmt.Errorf("failed to delete file %s: %w", path, err)
			}
		}

		return nil
	})
}
