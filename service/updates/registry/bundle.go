package registry

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/safing/portmaster/base/log"
)

const MaxUnpackSize = 1 << 30 // 2^30 == 1GB

const current_platform = runtime.GOOS + "_" + runtime.GOARCH

type Artifact struct {
	Filename string   `json:"Filename"`
	SHA256   string   `json:"SHA256"`
	URLs     []string `json:"URLs"`
	Platform string   `json:"Platform,omitempty"`
	Unpack   string   `json:"Unpack,omitempty"`
	Version  string   `json:"Version,omitempty"`
}

type Bundle struct {
	Name      string     `json:"Bundle"`
	Version   string     `json:"Version"`
	Published time.Time  `json:"Published"`
	Artifacts []Artifact `json:"Artifacts"`
}

func (bundle Bundle) downloadAndVerify(dir string) {
	client := http.Client{}
	for _, artifact := range bundle.Artifacts {

		filePath := fmt.Sprintf("%s/%s", dir, artifact.Filename)
		// TODO(vladimir): is this needed?
		_ = os.MkdirAll(filepath.Dir(filePath), defaultDirMode)

		// Check file is already downloaded and valid.
		exists, _ := checkIfFileIsValid(filePath, artifact)
		if exists {
			log.Debugf("updates: file already downloaded: %s", filePath)
			continue
		}

		// Download artifact
		err := processArtifact(&client, artifact, filePath)
		if err != nil {
			log.Errorf("updates: %s", err)
		}
	}
}

// Verify checks if the files are present int the dataDir and have the correct hash.
func (bundle Bundle) Verify(dir string) error {
	for _, artifact := range bundle.Artifacts {
		artifactPath := fmt.Sprintf("%s/%s", dir, artifact.Filename)
		file, err := os.Open(artifactPath)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", artifactPath, err)
		}
		defer func() { _ = file.Close() }()

		isValid, err := checkIfFileIsValid(artifactPath, artifact)
		if err != nil {
			return err
		}

		if !isValid {
			return fmt.Errorf("file is not valid: %s", artifact.Filename)
		}
	}

	return nil
}

func checkIfFileIsValid(filename string, artifact Artifact) (bool, error) {
	// Check if file already exists
	file, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()

	providedHash, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(providedHash) != sha256.Size {
		return false, fmt.Errorf("invalid provided hash %s: %w", artifact.SHA256, err)
	}

	// Calculate hash of the file
	fileHash := sha256.New()
	if _, err := io.Copy(fileHash, file); err != nil {
		return false, fmt.Errorf("failed to read file: %w", err)
	}
	hashInBytes := fileHash.Sum(nil)
	if !bytes.Equal(providedHash, hashInBytes) {
		return false, fmt.Errorf("file exist but the hash does not match: %s", filename)
	}
	return true, nil
}

func processArtifact(client *http.Client, artifact Artifact, filePath string) error {
	// Skip artifacts not meant for this machine.
	if artifact.Platform != "" && artifact.Platform != current_platform {
		return nil
	}

	providedHash, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(providedHash) != sha256.Size {
		return fmt.Errorf("invalid provided hash %s: %w", artifact.SHA256, err)
	}

	// Download
	content, err := downloadFile(client, artifact.URLs)
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
		// FIXME(vladimir): just for testing. Make it an error.
		err = fmt.Errorf("failed to verify artifact: %s", artifact.Filename)
		log.Debugf("updates: %s", err)
	}

	// Save
	tmpFilename := fmt.Sprintf("%s.download", filePath)
	file, err := os.Create(tmpFilename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	if artifact.Platform == "" {
		_ = file.Chmod(defaultFileMode)
	} else {
		_ = file.Chmod(executableFileMode)
	}
	_, err = file.Write(content)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}
	file.Close()

	// Rename
	err = os.Rename(tmpFilename, filePath)
	if err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

func downloadFile(client *http.Client, urls []string) ([]byte, error) {
	for _, url := range urls {
		// Try to make the request
		resp, err := client.Get(url)
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

func unpack(cType string, fileBytes []byte) ([]byte, error) {
	switch cType {
	case "zip":
		{
			return decompressZip(fileBytes)
		}
	case "gz":
		{
			return decompressGzip(fileBytes)
		}
	default:
		{
			return nil, fmt.Errorf("unsupported compression type")
		}
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
