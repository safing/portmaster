package updater

import (
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

// MaxUnpackSize specifies the maximum size that will be unpacked.
const MaxUnpackSize = 1000000000 // 1GB

// UnpackGZIP unpacks a GZIP compressed reader r
// and returns a new reader. It's suitable to be
// used with registry.GetPackedFile.
func UnpackGZIP(r io.Reader) (io.Reader, error) {
	return gzip.NewReader(r)
}

// UnpackResources unpacks all resources defined in the AutoUnpack list.
func (reg *ResourceRegistry) UnpackResources() error {
	reg.RLock()
	defer reg.RUnlock()

	var multierr *multierror.Error
	for _, res := range reg.resources {
		if utils.StringInSlice(reg.AutoUnpack, res.Identifier) {
			err := res.UnpackArchive()
			if err != nil {
				multierr = multierror.Append(
					multierr,
					fmt.Errorf("%s: %w", res.Identifier, err),
				)
			}
		}
	}

	return multierr.ErrorOrNil()
}

const (
	zipSuffix = ".zip"
)

// UnpackArchive unpacks the archive the resource refers to. The contents are
// unpacked into a directory with the same name as the file, excluding the
// suffix. If the destination folder already exists, it is assumed that the
// contents have already been correctly unpacked.
func (res *Resource) UnpackArchive() error {
	res.Lock()
	defer res.Unlock()

	// Only unpack selected versions.
	if res.SelectedVersion == nil {
		return nil
	}

	switch {
	case strings.HasSuffix(res.Identifier, zipSuffix):
		return res.unpackZipArchive()
	default:
		return fmt.Errorf("unsupported file type for unpacking")
	}
}

func (res *Resource) unpackZipArchive() error {
	// Get file and directory paths.
	archiveFile := res.SelectedVersion.storagePath()
	destDir := strings.TrimSuffix(archiveFile, zipSuffix)
	tmpDir := filepath.Join(
		res.registry.tmpDir.Path,
		filepath.FromSlash(strings.TrimSuffix(
			path.Base(res.SelectedVersion.versionedPath()),
			zipSuffix,
		)),
	)

	// Check status of destination.
	dstStat, err := os.Stat(destDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// The destination does not exist, continue with unpacking.
	case err != nil:
		return fmt.Errorf("cannot access destination for unpacking: %w", err)
	case !dstStat.IsDir():
		return fmt.Errorf("destination for unpacking is blocked by file: %s", dstStat.Name())
	default:
		// Archive already seems to be unpacked.
		return nil
	}

	// Create the tmp directory for unpacking.
	err = res.registry.tmpDir.EnsureAbsPath(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to create tmp dir for unpacking: %w", err)
	}

	// Defer clean up of directories.
	defer func() {
		// Always clean up the tmp dir.
		_ = os.RemoveAll(tmpDir)
		// Cleanup the destination in case of an error.
		if err != nil {
			_ = os.RemoveAll(destDir)
		}
	}()

	// Open the archive for reading.
	var archiveReader *zip.ReadCloser
	archiveReader, err = zip.OpenReader(archiveFile)
	if err != nil {
		return fmt.Errorf("failed to open zip reader: %w", err)
	}
	defer func() {
		_ = archiveReader.Close()
	}()

	// Save all files to the tmp dir.
	for _, file := range archiveReader.File {
		err = copyFromZipArchive(
			file,
			filepath.Join(tmpDir, filepath.FromSlash(file.Name)),
		)
		if err != nil {
			return fmt.Errorf("failed to extract archive file %s: %w", file.Name, err)
		}
	}

	// Make the final move.
	err = os.Rename(tmpDir, destDir)
	if err != nil {
		return fmt.Errorf("failed to move the extracted archive from %s to %s: %w", tmpDir, destDir, err)
	}

	// Fix permissions on the destination dir.
	err = res.registry.storageDir.EnsureAbsPath(destDir)
	if err != nil {
		return fmt.Errorf("failed to apply directory permissions on %s: %w", destDir, err)
	}

	log.Infof("%s: unpacked %s", res.registry.Name, res.SelectedVersion.versionedPath())
	return nil
}

func copyFromZipArchive(archiveFile *zip.File, dstPath string) error {
	// If file is a directory, create it and continue.
	if archiveFile.FileInfo().IsDir() {
		err := os.Mkdir(dstPath, archiveFile.Mode())
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
		}
		return nil
	}

	// Open archived file for reading.
	fileReader, err := archiveFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in archive: %w", err)
	}
	defer func() {
		_ = fileReader.Close()
	}()

	// Open destination file for writing.
	dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, archiveFile.Mode())
	if err != nil {
		return fmt.Errorf("failed to open destination file %s: %w", dstPath, err)
	}
	defer func() {
		_ = dstFile.Close()
	}()

	// Copy full file from archive to dst.
	if _, err := io.CopyN(dstFile, fileReader, MaxUnpackSize); err != nil {
		// EOF is expected here as the archive is likely smaller
		// thane MaxUnpackSize
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	return nil
}
