package updates

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const MaxUnpackSize = 1 << 30 // 2^30 == 1GB

const currentPlatform = runtime.GOOS + "_" + runtime.GOARCH

type Artifact struct {
	Filename string   `json:"Filename"`
	SHA256   string   `json:"SHA256"`
	URLs     []string `json:"URLs"`
	Platform string   `json:"Platform,omitempty"`
	Unpack   string   `json:"Unpack,omitempty"`
	Version  string   `json:"Version,omitempty"`
}

func (a *Artifact) GetFileMode() os.FileMode {
	// Special case for portmaster ui. Should be able to be executed from the regular user
	if a.Platform == currentPlatform && a.Filename == "portmaster" {
		return executableUIFileMode
	}

	if a.Platform == currentPlatform {
		return executableFileMode
	}

	return defaultFileMode
}

type Bundle struct {
	Name      string     `json:"Bundle"`
	Version   string     `json:"Version"`
	Published time.Time  `json:"Published"`
	Artifacts []Artifact `json:"Artifacts"`
}

func ParseBundle(indexFile string) (*Bundle, error) {
	// Check if the file exists.
	file, err := os.Open(indexFile)
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
		return nil, fmt.Errorf("%s %w", indexFile, err)
	}

	// Filter artifacts
	filtered := make([]Artifact, 0)
	for _, a := range bundle.Artifacts {
		if a.Platform == "" || a.Platform == currentPlatform {
			filtered = append(filtered, a)
		}
	}
	bundle.Artifacts = filtered

	return &bundle, nil
}

// Verify checks if the files are present int the dataDir and have the correct hash.
func (bundle Bundle) Verify(dir string) error {
	for _, artifact := range bundle.Artifacts {
		artifactPath := filepath.Join(dir, artifact.Filename)
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

// GenerateBundleFromDir generates a bundle from a given folder.
func GenerateBundleFromDir(name string, version string, properties map[string]Artifact, dirPath string) (*Bundle, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0, len(files))
	for _, f := range files {
		// Skip dirs
		if f.IsDir() {
			continue
		}

		artifact := Artifact{
			Filename: f.Name(),
		}
		predefined, ok := properties[f.Name()]
		// Check if caller supplied predefined settings for this artifact.
		if ok {
			// File that have compression may have different filename and artifact filename. (because of the extension)
			// If caller did not specify the artifact filename set it as the same as the filename.
			if predefined.Filename == "" {
				predefined.Filename = f.Name()
			}
			artifact = predefined
		}
		content, err := os.ReadFile(filepath.Join(dirPath, f.Name()))
		if err != nil {
			return nil, err
		}

		// Decompress if compression was applied to the file.
		if artifact.Unpack != "" {
			content, err = unpack(artifact.Unpack, content)
			if err != nil {
				return nil, err
			}
		}

		hash := sha256.Sum256(content)
		hashStr := hex.EncodeToString(hash[:])
		artifact.SHA256 = hashStr

		artifacts = append(artifacts, artifact)
	}

	return &Bundle{
		Name:      name,
		Version:   version,
		Artifacts: artifacts,
		Published: time.Now(),
	}, nil
}
