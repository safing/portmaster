package updates

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

type Downloader struct {
	u         *Updater
	index     *Index
	indexURLs []string

	existingFiles map[string]string

	httpClient http.Client
}

func NewDownloader(u *Updater, indexURLs []string) *Downloader {
	return &Downloader{
		u:         u,
		indexURLs: indexURLs,
	}
}

func (d *Downloader) updateIndex(ctx context.Context) error {
	// Make sure dir exists.
	err := utils.EnsureDirectory(d.u.cfg.DownloadDirectory, utils.PublicReadExecPermission)
	if err != nil {
		return fmt.Errorf("create download directory: %s", d.u.cfg.DownloadDirectory)
	}

	// Try to download the index from one of the index URLs.
	var (
		indexData []byte
		index     *Index
	)
	for _, url := range d.indexURLs {
		// Download and verify index.
		indexData, index, err = d.getIndex(ctx, url)
		if err == nil {
			// Valid index found!
			break
		}

		log.Warningf("updates/%s: failed to update index from %q: %s", d.u.cfg.Name, url, err)
		err = fmt.Errorf("update index file from %q: %w", url, err)
	}
	if err != nil {
		return fmt.Errorf("all index URLs failed, last error: %w", err)
	}
	d.index = index

	// Write the index into a file.
	indexFilepath := filepath.Join(d.u.cfg.DownloadDirectory, d.u.cfg.IndexFile)
	err = os.WriteFile(indexFilepath, indexData, utils.PublicReadExecPermission.AsUnixPermission())
	if err != nil {
		return fmt.Errorf("write index file: %w", err)
	}

	return nil
}

func (d *Downloader) getIndex(ctx context.Context, url string) (indexData []byte, bundle *Index, err error) {
	// Download data from URL.
	indexData, err = d.downloadData(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("GET index: %w", err)
	}

	// Verify and parse index.
	bundle, err = ParseIndex(indexData, d.u.cfg.Verify)
	if err != nil {
		return nil, nil, fmt.Errorf("parse index: %w", err)
	}

	return indexData, bundle, nil
}

// gatherExistingFiles gathers the checksums on existing files.
func (d *Downloader) gatherExistingFiles(dir string) error {
	// Make sure map is initialized.
	if d.existingFiles == nil {
		d.existingFiles = make(map[string]string)
	}

	// Walk directory, just log errors.
	err := filepath.WalkDir(dir, func(fullpath string, entry fs.DirEntry, err error) error {
		// Fail on access error.
		if err != nil {
			return err
		}

		// Skip folders.
		if entry.IsDir() {
			return nil
		}

		// Read full file.
		fileData, err := os.ReadFile(fullpath)
		if err != nil {
			log.Debugf("updates/%s: failed to read file %q while searching for existing files: %s", d.u.cfg.Name, fullpath, err)
			return fmt.Errorf("failed to read file %s: %w", fullpath, err)
		}

		// Calculate checksum and add it to the existing files.
		hashSum := sha256.Sum256(fileData)
		d.existingFiles[hex.EncodeToString(hashSum[:])] = fullpath

		return nil
	})
	if err != nil {
		return fmt.Errorf("searching for existing files: %w", err)
	}

	return nil
}

func (d *Downloader) downloadArtifacts(ctx context.Context) error {
	// Make sure dir exists.
	err := utils.EnsureDirectory(d.u.cfg.DownloadDirectory, utils.PublicReadExecPermission)
	if err != nil {
		return fmt.Errorf("create download directory: %s", d.u.cfg.DownloadDirectory)
	}

artifacts:
	for _, artifact := range d.index.Artifacts {
		dstFilePath := filepath.Join(d.u.cfg.DownloadDirectory, artifact.Filename)

		// Check if we can copy the artifact from disk instead.
		if existingFile, ok := d.existingFiles[artifact.SHA256]; ok {
			// Check if this is the same file.
			if existingFile == dstFilePath {
				continue artifacts
			}
			// Copy and check.
			err = copyAndCheckSHA256Sum(existingFile, dstFilePath, artifact.SHA256, artifact.GetFileMode())
			if err == nil {
				continue artifacts
			}
			log.Debugf("updates/%s: failed to copy existing file %s: %s", d.u.cfg.Name, artifact.Filename, err)
		}

		// Check if the artifact has download URLs.
		if len(artifact.URLs) == 0 {
			return fmt.Errorf("artifact %s is missing download URLs", artifact.Filename)
		}

		// Try to download the artifact from one of the URLs.
		var artifactData []byte
	artifactURLs:
		for _, url := range artifact.URLs {
			// Download and verify index.
			artifactData, err = d.getArtifact(ctx, artifact, url)
			if err == nil {
				// Valid artifact found!
				break artifactURLs
			}
			err = fmt.Errorf("update index file from %q: %w", url, err)
		}
		if err != nil {
			return fmt.Errorf("all artifact URLs for %s failed, last error: %w", artifact.Filename, err)
		}

		// Write artifact to temporary file.
		tmpFilename := dstFilePath + ".download"
		err = os.WriteFile(tmpFilename, artifactData, artifact.GetFileMode().AsUnixPermission())
		if err != nil {
			return fmt.Errorf("write %s to temp file: %w", artifact.Filename, err)
		}

		_ = utils.SetFilePermission(tmpFilename, artifact.GetFileMode())

		// Rename/Move to actual location.
		err = os.Rename(tmpFilename, dstFilePath)
		if err != nil {
			return fmt.Errorf("rename %s after write: %w", artifact.Filename, err)
		}

		log.Infof("updates/%s: downloaded and verified %s", d.u.cfg.Name, artifact.Filename)
	}
	return nil
}

func (d *Downloader) getArtifact(ctx context.Context, artifact *Artifact, url string) ([]byte, error) {
	// Download data from URL.
	artifactData, err := d.downloadData(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("GET artifact: %w", err)
	}

	// Decompress artifact data, if configured.
	// TODO: Normally we should do operations on "untrusted" data _after_ verification,
	// but we really want the checksum to be for the unpacked data. Should we add another checksum, or is HTTPS enough?
	if artifact.Unpack != "" {
		artifactData, err = decompress(artifact.Unpack, artifactData)
		if err != nil {
			return nil, fmt.Errorf("decompress: %w", err)
		}
	}

	// Verify checksum.
	if err := checkSHA256Sum(artifactData, artifact.SHA256); err != nil {
		return nil, err
	}

	return artifactData, nil
}

func (d *Downloader) downloadData(ctx context.Context, url string) ([]byte, error) {
	// Setup request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request to %s: %w", url, err)
	}
	if UserAgent != "" {
		req.Header.Set("User-Agent", UserAgent)
	}

	// Start request with shared http client.
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed a get file request to: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for HTTP status errors.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned non-OK status: %d %s", resp.StatusCode, resp.Status)
	}

	// Read the full body and return it.
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body of response: %w", err)
	}
	return content, nil
}

func decompress(cType string, fileBytes []byte) ([]byte, error) {
	switch cType {
	case "zip":
		return decompressZip(fileBytes)
	case "gz":
		return decompressGzip(fileBytes)
	default:
		return nil, fmt.Errorf("unsupported compression type")
	}
}

func decompressGzip(data []byte) ([]byte, error) {
	// Create a gzip reader from the byte slice.
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	// Copy from the gzip reader into a new buffer.
	var buf bytes.Buffer
	_, err = io.CopyN(&buf, gzipReader, MaxUnpackSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read gzip file: %w", err)
	}

	return buf.Bytes(), nil
}

func decompressZip(data []byte) ([]byte, error) {
	// Create a zip reader from the byte slice.
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("create zip reader: %w", err)
	}

	// Ensure there is only one file in the zip.
	if len(zipReader.File) != 1 {
		return nil, fmt.Errorf("zip file must contain exactly one file")
	}

	// Open single file in the zip.
	file := zipReader.File[0]
	fileReader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open file in zip: %w", err)
	}
	defer func() { _ = fileReader.Close() }()

	// Copy from the zip reader into a new buffer.
	var buf bytes.Buffer
	_, err = io.CopyN(&buf, fileReader, MaxUnpackSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read file in zip: %w", err)
	}

	return buf.Bytes(), nil
}
