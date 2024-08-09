package updater

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/jess/filesig"
	"github.com/safing/jess/lhash"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

// ScanStorage scans root within the storage dir and adds found
// resources to the registry. If an error occurred, it is logged
// and the last error is returned. Everything that was found
// despite errors is added to the registry anyway. Leave root
// empty to scan the full storage dir.
func (reg *ResourceRegistry) ScanStorage(root string) error {
	var lastError error

	// prep root
	if root == "" {
		root = reg.storageDir.Path
	} else {
		var err error
		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(root, reg.storageDir.Path) {
			return errors.New("supplied scan root path not within storage")
		}
	}

	// walk fs
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// skip tmp dir (including errors trying to read it)
		if strings.HasPrefix(path, reg.tmpDir.Path) {
			return filepath.SkipDir
		}

		// handle walker error
		if err != nil {
			lastError = fmt.Errorf("%s: could not read %s: %w", reg.Name, path, err)
			log.Warning(lastError.Error())
			return nil
		}

		// Ignore file signatures.
		if strings.HasSuffix(path, filesig.Extension) {
			return nil
		}

		// get relative path to storage
		relativePath, err := filepath.Rel(reg.storageDir.Path, path)
		if err != nil {
			lastError = fmt.Errorf("%s: could not get relative path of %s: %w", reg.Name, path, err)
			log.Warning(lastError.Error())
			return nil
		}

		// convert to identifier and version
		relativePath = filepath.ToSlash(relativePath)
		identifier, version, ok := GetIdentifierAndVersion(relativePath)
		if !ok {
			// file does not conform to format
			return nil
		}

		// fully ignore directories that also have an identifier - these will be unpacked resources
		if info.IsDir() {
			return filepath.SkipDir
		}

		// save
		err = reg.AddResource(identifier, version, nil, true, false, false)
		if err != nil {
			lastError = fmt.Errorf("%s: could not get add resource %s v%s: %w", reg.Name, identifier, version, err)
			log.Warning(lastError.Error())
		}
		return nil
	})

	return lastError
}

// LoadIndexes loads the current release indexes from disk
// or will fetch a new version if not available and the
// registry is marked as online.
func (reg *ResourceRegistry) LoadIndexes(ctx context.Context) error {
	var firstErr error
	client := &http.Client{}
	for _, idx := range reg.getIndexes() {
		err := reg.loadIndexFile(idx)
		if err == nil {
			log.Debugf("%s: loaded index %s", reg.Name, idx.Path)
		} else if reg.Online {
			// try to download the index file if a local disk version
			// does not exist or we don't have permission to read it.
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				err = reg.downloadIndex(ctx, client, idx)
			}
		}

		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// getIndexes returns a copy of the index.
// The indexes itself are references.
func (reg *ResourceRegistry) getIndexes() []*Index {
	reg.RLock()
	defer reg.RUnlock()

	indexes := make([]*Index, len(reg.indexes))
	copy(indexes, reg.indexes)
	return indexes
}

func (reg *ResourceRegistry) loadIndexFile(idx *Index) error {
	indexPath := filepath.Join(reg.storageDir.Path, filepath.FromSlash(idx.Path))
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read index file %s: %w", idx.Path, err)
	}

	// Verify signature, if enabled.
	if verifOpts := reg.GetVerificationOptions(idx.Path); verifOpts != nil {
		// Load and check signature.
		verifiedHash, _, err := reg.loadAndVerifySigFile(verifOpts, indexPath+filesig.Extension)
		if err != nil {
			switch verifOpts.DiskLoadPolicy {
			case SignaturePolicyRequire:
				return fmt.Errorf("failed to verify signature of index %s: %w", idx.Path, err)
			case SignaturePolicyWarn:
				log.Warningf("%s: failed to verify signature of index %s: %s", reg.Name, idx.Path, err)
			case SignaturePolicyDisable:
				log.Debugf("%s: failed to verify signature of index %s: %s", reg.Name, idx.Path, err)
			}
		}

		// Check if signature checksum matches the index data.
		if err == nil && !verifiedHash.Matches(indexData) {
			switch verifOpts.DiskLoadPolicy {
			case SignaturePolicyRequire:
				return fmt.Errorf("index file %s does not match signature", idx.Path)
			case SignaturePolicyWarn:
				log.Warningf("%s: index file %s does not match signature", reg.Name, idx.Path)
			case SignaturePolicyDisable:
				log.Debugf("%s: index file %s does not match signature", reg.Name, idx.Path)
			}
		}
	}

	// Parse the index file.
	indexFile, err := ParseIndexFile(indexData, idx.Channel, idx.LastRelease)
	if err != nil {
		return fmt.Errorf("failed to parse index file %s: %w", idx.Path, err)
	}

	// Update last seen release.
	idx.LastRelease = indexFile.Published

	// Warn if there aren't any releases in the index.
	if len(indexFile.Releases) == 0 {
		log.Debugf("%s: index %s has no releases", reg.Name, idx.Path)
		return nil
	}

	// Add index releases to available resources.
	err = reg.AddResources(indexFile.Releases, idx, false, true, idx.PreRelease)
	if err != nil {
		log.Warningf("%s: failed to add resource: %s", reg.Name, err)
	}
	return nil
}

func (reg *ResourceRegistry) loadAndVerifySigFile(verifOpts *VerificationOptions, sigFilePath string) (*lhash.LabeledHash, []byte, error) {
	// Load signature file.
	sigFileData, err := os.ReadFile(sigFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read signature file: %w", err)
	}

	// Extract all signatures.
	sigs, err := filesig.ParseSigFile(sigFileData)
	switch {
	case len(sigs) == 0 && err != nil:
		return nil, nil, fmt.Errorf("failed to parse signature file: %w", err)
	case len(sigs) == 0:
		return nil, nil, errors.New("no signatures found in signature file")
	case err != nil:
		return nil, nil, fmt.Errorf("failed to parse signature file: %w", err)
	}

	// Verify all signatures.
	var verifiedHash *lhash.LabeledHash
	for _, sig := range sigs {
		fd, err := filesig.VerifyFileData(
			sig,
			nil,
			verifOpts.TrustStore,
		)
		if err != nil {
			return nil, sigFileData, err
		}

		// Save or check verified hash.
		if verifiedHash == nil {
			verifiedHash = fd.FileHash()
		} else if !fd.FileHash().Equal(verifiedHash) {
			// Return an error if two valid hashes mismatch.
			// For simplicity, all hash algorithms must be the same for now.
			return nil, sigFileData, errors.New("file hashes from different signatures do not match")
		}
	}

	return verifiedHash, sigFileData, nil
}

// CreateSymlinks creates a directory structure with unversioned symlinks to the given updates list.
func (reg *ResourceRegistry) CreateSymlinks(symlinkRoot *utils.DirStructure) error {
	err := os.RemoveAll(symlinkRoot.Path)
	if err != nil {
		return fmt.Errorf("failed to wipe symlink root: %w", err)
	}

	err = symlinkRoot.Ensure()
	if err != nil {
		return fmt.Errorf("failed to create symlink root: %w", err)
	}

	reg.RLock()
	defer reg.RUnlock()

	for _, res := range reg.resources {
		if res.SelectedVersion == nil {
			return fmt.Errorf("no selected version available for %s", res.Identifier)
		}

		targetPath := res.SelectedVersion.storagePath()
		linkPath := filepath.Join(symlinkRoot.Path, filepath.FromSlash(res.Identifier))
		linkPathDir := filepath.Dir(linkPath)

		err = symlinkRoot.EnsureAbsPath(linkPathDir)
		if err != nil {
			return fmt.Errorf("failed to create dir for link: %w", err)
		}

		relativeTargetPath, err := filepath.Rel(linkPathDir, targetPath)
		if err != nil {
			return fmt.Errorf("failed to get relative target path: %w", err)
		}

		err = os.Symlink(relativeTargetPath, linkPath)
		if err != nil {
			return fmt.Errorf("failed to link %s: %w", res.Identifier, err)
		}
	}

	return nil
}
