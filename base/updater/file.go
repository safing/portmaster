package updater

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	semver "github.com/hashicorp/go-version"

	"github.com/safing/jess/filesig"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

// File represents a file from the update system.
type File struct {
	resource      *Resource
	version       *ResourceVersion
	notifier      *notifier
	versionedPath string
	storagePath   string
}

// Identifier returns the identifier of the file.
func (file *File) Identifier() string {
	return file.resource.Identifier
}

// Version returns the version of the file.
func (file *File) Version() string {
	return file.version.VersionNumber
}

// SemVer returns the semantic version of the file.
func (file *File) SemVer() *semver.Version {
	return file.version.semVer
}

// EqualsVersion normalizes the given version and checks equality with semver.
func (file *File) EqualsVersion(version string) bool {
	return file.version.EqualsVersion(version)
}

// Path returns the absolute filepath of the file.
func (file *File) Path() string {
	return file.storagePath
}

// SigningMetadata returns the metadata to be included in signatures.
func (file *File) SigningMetadata() map[string]string {
	return map[string]string{
		"id":      file.Identifier(),
		"version": file.Version(),
	}
}

// Verify verifies the given file.
func (file *File) Verify() ([]*filesig.FileData, error) {
	// Check if verification is configured.
	if file.resource.VerificationOptions == nil {
		return nil, ErrVerificationNotConfigured
	}

	// Verify file.
	fileData, err := filesig.VerifyFile(
		file.storagePath,
		file.storagePath+filesig.Extension,
		file.SigningMetadata(),
		file.resource.VerificationOptions.TrustStore,
	)
	if err != nil {
		switch file.resource.VerificationOptions.DiskLoadPolicy {
		case SignaturePolicyRequire:
			return nil, err
		case SignaturePolicyWarn:
			log.Warningf("%s: failed to verify %s: %s", file.resource.registry.Name, file.storagePath, err)
		case SignaturePolicyDisable:
			log.Debugf("%s: failed to verify %s: %s", file.resource.registry.Name, file.storagePath, err)
		}
	}

	return fileData, nil
}

// Blacklist notifies the update system that this file is somehow broken, and should be ignored from now on, until restarted.
func (file *File) Blacklist() error {
	return file.resource.Blacklist(file.version.VersionNumber)
}

// markActiveWithLocking marks the file as active, locking the resource in the process.
func (file *File) markActiveWithLocking() {
	file.resource.Lock()
	defer file.resource.Unlock()

	// update last used version
	if file.resource.ActiveVersion != file.version {
		log.Debugf("updater: setting active version of resource %s from %s to %s", file.resource.Identifier, file.resource.ActiveVersion, file.version.VersionNumber)
		file.resource.ActiveVersion = file.version
	}
}

// Unpacker describes the function that is passed to
// File.Unpack. It receives a reader to the compressed/packed
// file and should return a reader that provides
// unpacked file contents. If the returned reader implements
// io.Closer it's close method is invoked when an error
// or io.EOF is returned from Read().
type Unpacker func(io.Reader) (io.Reader, error)

// Unpack returns the path to the unpacked version of file and
// unpacks it on demand using unpacker.
func (file *File) Unpack(suffix string, unpacker Unpacker) (string, error) {
	path := strings.TrimSuffix(file.Path(), suffix)

	if suffix == "" {
		path += "-unpacked"
	}

	_, err := os.Stat(path)
	if err == nil {
		return path, nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	f, err := os.Open(file.Path())
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	r, err := unpacker(f)
	if err != nil {
		return "", err
	}

	ioErr := utils.CreateAtomic(path, r, &utils.AtomicFileOptions{
		TempDir: file.resource.registry.TmpDir().Path,
	})

	if c, ok := r.(io.Closer); ok {
		if err := c.Close(); err != nil && ioErr == nil {
			// if ioErr is already set we ignore the error from
			// closing the unpacker.
			ioErr = err
		}
	}

	return path, ioErr
}
