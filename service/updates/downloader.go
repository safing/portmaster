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
	"net/http"
	"os"
	"path/filepath"
	"strings"

	semver "github.com/hashicorp/go-version"

	"github.com/safing/portmaster/base/log"
)

type Downloader struct {
	dir       string
	indexURLs []string
	bundle    *Bundle
	version   *semver.Version

	httpClient http.Client
}

func CreateDownloader(index UpdateIndex) Downloader {
	return Downloader{
		dir:       index.DownloadDirectory,
		indexURLs: index.IndexURLs,
	}
}

func (d *Downloader) downloadIndexFile(ctx context.Context) error {
	// Make sure dir exists
	err := os.MkdirAll(d.dir, defaultDirMode)
	if err != nil {
		return fmt.Errorf("failed to create directory for updates: %s", d.dir)
	}
	var content string
	for _, url := range d.indexURLs {
		content, err = d.downloadIndexFileFromURL(ctx, url)
		if err != nil {
			log.Warningf("updates: failed while downloading index file: %s", err)
			continue
		}
		// Downloading was successful.
		var bundle *Bundle
		bundle, err = ParseBundle(content)
		if err != nil {
			log.Warningf("updates: %s", err)
			continue
		}
		// Parsing was successful
		var version *semver.Version
		version, err = semver.NewVersion(bundle.Version)
		if err != nil {
			log.Warningf("updates: failed to parse bundle version: %s", err)
			continue
		}

		// All checks passed. Set and exit the loop.
		d.bundle = bundle
		d.version = version
		err = nil
		break
	}

	if err != nil {
		return err
	}

	// Write the content into a file.
	indexFilepath := filepath.Join(d.dir, indexFilename)
	err = os.WriteFile(indexFilepath, []byte(content), defaultFileMode)
	if err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

// Verify verifies if the downloaded files match the corresponding hash.
func (d *Downloader) Verify() error {
	err := d.parseBundle()
	if err != nil {
		return err
	}

	return d.bundle.Verify(d.dir)
}

func (d *Downloader) parseBundle() error {
	indexFilepath := filepath.Join(d.dir, indexFilename)
	var err error
	d.bundle, err = LoadBundle(indexFilepath)
	if err != nil {
		return err
	}

	d.version, err = semver.NewVersion(d.bundle.Version)
	if err != nil {
		return err
	}
	return nil
}

func (d *Downloader) downloadIndexFileFromURL(ctx context.Context, url string) (string, error) {
	// Request the index file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create GET request to: %w", err)
	}
	if UserAgent != "" {
		req.Header.Set("User-Agent", UserAgent)
	}

	// Perform request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed GET request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check the status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("received error from the server status code: %s", resp.Status)
	}

	// Read the content.
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// CopyMatchingFilesFromCurrent check if there the current bundle files has matching files with the new bundle and copies them if they match.
func (d *Downloader) copyMatchingFilesFromCurrent(currentFiles map[string]File) error {
	// Make sure new dir exists
	_ = os.MkdirAll(d.dir, defaultDirMode)

	for _, a := range d.bundle.Artifacts {
		currentFile, ok := currentFiles[a.Filename]
		if ok && currentFile.Sha256() == a.SHA256 {
			// Read the content of the current file.
			content, err := os.ReadFile(currentFile.Path())
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", currentFile.Path(), err)
			}

			// Check if the content matches the artifact hash
			expectedHash, err := hex.DecodeString(a.SHA256)
			if err != nil || len(expectedHash) != sha256.Size {
				return fmt.Errorf("invalid artifact hash %s: %w", a.SHA256, err)
			}
			hash := sha256.Sum256(content)
			if !bytes.Equal(expectedHash, hash[:]) {
				return fmt.Errorf("expected and file hash mismatch: %s", currentFile.Path())
			}

			// Create new file
			destFilePath := filepath.Join(d.dir, a.Filename)
			err = os.WriteFile(destFilePath, content, a.GetFileMode())
			if err != nil {
				return fmt.Errorf("failed to write to file %s: %w", destFilePath, err)
			}
			log.Debugf("updates: file copied from current version: %s", a.Filename)
		}
	}
	return nil
}

func (d *Downloader) downloadAndVerify(ctx context.Context) error {
	// Make sure we have the bundle file parsed.
	err := d.parseBundle()
	if err != nil {
		return fmt.Errorf("invalid update bundle file: %w", err)
	}

	// Make sure dir exists
	_ = os.MkdirAll(d.dir, defaultDirMode)

	for _, artifact := range d.bundle.Artifacts {
		filePath := filepath.Join(d.dir, artifact.Filename)

		// Check file is already downloaded and valid.
		exists, _ := checkIfFileIsValid(filePath, artifact)
		if exists {
			log.Debugf("updates: file already downloaded: %s", filePath)
			continue
		}

		// Download artifact
		err := d.processArtifact(ctx, artifact, filePath)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Downloader) processArtifact(ctx context.Context, artifact Artifact, filePath string) error {
	providedHash, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(providedHash) != sha256.Size {
		return fmt.Errorf("invalid provided hash %s: %w", artifact.SHA256, err)
	}

	// Download and verify
	log.Debugf("updates: downloading file: %s", artifact.Filename)
	content, err := d.downloadAndVerifyArtifact(ctx, artifact.URLs, artifact.Unpack, providedHash)
	if err != nil {
		return fmt.Errorf("failed to download artifact: %w", err)
	}

	// Save
	tmpFilename := fmt.Sprintf("%s.download", filePath)
	err = os.WriteFile(tmpFilename, content, artifact.GetFileMode())
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	// Rename
	err = os.Rename(tmpFilename, filePath)
	if err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	log.Infof("updates: file downloaded and verified: %s", artifact.Filename)

	return nil
}

func (d *Downloader) downloadAndVerifyArtifact(ctx context.Context, urls []string, unpack string, expectedHash []byte) ([]byte, error) {
	var err error
	var content []byte

	for _, url := range urls {
		// Download
		content, err = d.downloadFile(ctx, url)
		if err != nil {
			err := fmt.Errorf("failed to download artifact from url: %s, %w", url, err)
			log.Warningf("%s", err)
			continue
		}

		// Decompress
		if unpack != "" {
			content, err = decompress(unpack, content)
			if err != nil {
				err = fmt.Errorf("failed to decompress artifact: %w", err)
				log.Warningf("%s", err)
				continue
			}
		}

		// Calculate and verify hash
		hash := sha256.Sum256(content)
		if !bytes.Equal(expectedHash, hash[:]) {
			err := fmt.Errorf("artifact hash does not match")
			log.Warningf("%s", err)
			continue
		}

		// All file downloaded and verified.
		return content, nil
	}

	return nil, err
}

func (d *Downloader) downloadFile(ctx context.Context, url string) ([]byte, error) {
	// Try to make the request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request to %s: %w", url, err)
	}
	if UserAgent != "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed a get file request to: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check if the server returned an error
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned non-OK status: %d %s", resp.StatusCode, resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body of response: %w", err)
	}
	return content, nil
}

func (d *Downloader) deleteUnfinishedDownloads() error {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// Check if the current file has the download extension
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".download") {
			path := filepath.Join(d.dir, e.Name())
			log.Warningf("updates: deleting unfinished download file: %s\n", path)
			err := os.Remove(path)
			if err != nil {
				log.Errorf("updates: failed to delete unfinished download file %s: %s", path, err)
			}
		}
	}
	return nil
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
	// Create a gzip reader from the byte array
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	var buf bytes.Buffer
	_, err = io.CopyN(&buf, gzipReader, MaxUnpackSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read gzip file: %w", err)
	}

	return buf.Bytes(), nil
}

func decompressZip(data []byte) ([]byte, error) {
	// Create a zip reader from the byte array
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}

	// Ensure there is only one file in the zip
	if len(zipReader.File) != 1 {
		return nil, fmt.Errorf("zip file must contain exactly one file")
	}

	// Read the single file in the zip
	file := zipReader.File[0]
	fileReader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer func() { _ = fileReader.Close() }()

	var buf bytes.Buffer
	_, err = io.CopyN(&buf, fileReader, MaxUnpackSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read file in zip: %w", err)
	}

	return buf.Bytes(), nil
}
