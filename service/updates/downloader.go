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
	indexFile string
	indexURLs []string
	bundle    *Bundle
	version   *semver.Version

	httpClient http.Client
}

func CreateDownloader(index UpdateIndex) Downloader {
	return Downloader{
		dir:       index.DownloadDirectory,
		indexFile: index.IndexFile,
		indexURLs: index.IndexURLs,
	}
}

func (d *Downloader) downloadIndexFile(ctx context.Context) (err error) {
	// Make sure dir exists
	_ = os.MkdirAll(d.dir, defaultDirMode)
	for _, url := range d.indexURLs {
		err = d.downloadIndexFileFromURL(ctx, url)
		if err != nil {
			log.Warningf("updates: failed while downloading index file %s", err)
			continue
		}
		// Downloading was successful.
		err = nil
		break
	}

	if err == nil {
		err = d.parseBundle()
	}

	return
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
	indexFilepath := filepath.Join(d.dir, d.indexFile)
	var err error
	d.bundle, err = ParseBundle(indexFilepath)
	if err != nil {
		return err
	}

	d.version, err = semver.NewVersion(d.bundle.Version)
	if err != nil {
		return err
	}
	return nil
}

func (d *Downloader) downloadIndexFileFromURL(ctx context.Context, url string) error {
	// Request the index file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create GET request to %s: %w", url, err)
	}
	if UserAgent != "" {
		req.Header.Set("User-Agent", UserAgent)
	}

	// Perform request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed GET request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check the status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("received error from the server status code: %s", resp.Status)
	}
	// Create file
	indexFilepath := filepath.Join(d.dir, d.indexFile)
	file, err := os.Create(indexFilepath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	// Write body of the response
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	return nil
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
		log.Errorf("updates: invalid update bundle file: %s", err)
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

	// Download
	log.Debugf("updates: downloading file: %s", artifact.Filename)
	content, err := d.downloadFile(ctx, artifact.URLs)
	if err != nil {
		return fmt.Errorf("failed to download artifact: %w", err)
	}

	// Decompress
	if artifact.Unpack != "" {
		content, err = unpack(artifact.Unpack, content)
		if err != nil {
			return fmt.Errorf("failed to decompress artifact: %w", err)
		}
	}

	// Verify
	hash := sha256.Sum256(content)
	if !bytes.Equal(providedHash, hash[:]) {
		return fmt.Errorf("failed to verify artifact: %s", artifact.Filename)
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

func (d *Downloader) downloadFile(ctx context.Context, urls []string) ([]byte, error) {
	for _, url := range urls {
		// Try to make the request
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			log.Warningf("failed to create GET request to %s: %s", url, err)
			continue
		}
		if UserAgent != "" {
			req.Header.Set("User-Agent", UserAgent)
		}
		resp, err := d.httpClient.Do(req)
		if err != nil {
			log.Warningf("failed a get file request to: %s", err)
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		// Check if the server returned an error
		if resp.StatusCode != http.StatusOK {
			log.Warningf("server returned non-OK status: %d %s", resp.StatusCode, resp.Status)
			continue
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Warningf("failed to read body of response: %s", err)
			continue
		}
		return content, nil
	}

	return nil, fmt.Errorf("failed to download file from the provided urls")
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

func unpack(cType string, fileBytes []byte) ([]byte, error) {
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
