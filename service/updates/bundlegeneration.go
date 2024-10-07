package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	semver "github.com/hashicorp/go-version"
)

type BundleFileSettings struct {
	Name        string
	Version     string
	Properties  map[string]Artifact
	IgnoreFiles map[string]struct{}
}

// GenerateBundleFromDir generates a bundle from a given folder.
func GenerateBundleFromDir(bundleDir string, settings BundleFileSettings) (*Bundle, error) {
	bundleDirName := filepath.Base(bundleDir)

	artifacts := make([]Artifact, 0, 5)
	err := filepath.Walk(bundleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip folders
		if info.IsDir() {
			return nil
		}

		identifier, version, ok := getIdentifierAndVersion(info.Name())
		if !ok {
			identifier = info.Name()
		}

		// Check if file is in the ignore list.
		if _, ok := settings.IgnoreFiles[identifier]; ok {
			return nil
		}

		artifact := Artifact{}

		// Check if the caller provided properties for the artifact.
		if p, ok := settings.Properties[identifier]; ok {
			artifact = p
		}

		// Set filename of artifact if not set by the caller.
		if artifact.Filename == "" {
			artifact.Filename = identifier
		}

		artifact.Version = version

		// Fill the platform of the artifact
		parentDir := filepath.Base(filepath.Dir(path))
		if parentDir != "all" && parentDir != bundleDirName {
			artifact.Platform = parentDir
		}

		// Fill the hash
		hash, err := getSHA256(path, artifact.Unpack)
		if err != nil {
			return fmt.Errorf("failed to calculate hash of file: %s %w", path, err)
		}
		artifact.SHA256 = hash

		artifacts = append(artifacts, artifact)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk the dir: %w", err)
	}

	// Filter artifact so we have single version for each file
	artifacts, err = selectLatestArtifacts(artifacts)
	if err != nil {
		return nil, fmt.Errorf("failed to select artifact version: %w", err)
	}

	return &Bundle{
		Name:      settings.Name,
		Version:   settings.Version,
		Artifacts: artifacts,
		Published: time.Now(),
	}, nil
}

func selectLatestArtifacts(artifacts []Artifact) ([]Artifact, error) {
	artifactsMap := make(map[string]Artifact)

	for _, a := range artifacts {
		// Make the key platform specific since there can be same filename for multiple platforms.
		key := a.Filename + a.Platform
		aMap, ok := artifactsMap[key]
		if !ok {
			artifactsMap[key] = a
			continue
		}

		if aMap.Version == "" || a.Version == "" {
			return nil, fmt.Errorf("invalid mix version and non versioned files for: %s", a.Filename)
		}

		mapVersion, err := semver.NewVersion(aMap.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid version for artifact: %s", aMap.Filename)
		}

		artifactVersion, err := semver.NewVersion(a.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid version for artifact: %s", a.Filename)
		}

		if mapVersion.LessThan(artifactVersion) {
			artifactsMap[key] = a
		}
	}

	artifactsFiltered := make([]Artifact, 0, len(artifactsMap))
	for _, a := range artifactsMap {
		artifactsFiltered = append(artifactsFiltered, a)
	}

	return artifactsFiltered, nil
}

func getSHA256(path string, unpackType string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Decompress if compression was applied to the file.
	if unpackType != "" {
		content, err = unpack(unpackType, content)
		if err != nil {
			return "", err
		}
	}

	// Calculate hash
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:]), nil
}

var (
	fileVersionRegex = regexp.MustCompile(`_v[0-9]+-[0-9]+-[0-9]+(-[a-z]+)?`)
	rawVersionRegex  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+)?$`)
)

func getIdentifierAndVersion(versionedPath string) (identifier, version string, ok bool) {
	dirPath, filename := path.Split(versionedPath)

	// Extract version from filename.
	rawVersion := fileVersionRegex.FindString(filename)
	if rawVersion == "" {
		// No version present in file, making it invalid.
		return "", "", false
	}

	// Trim the `_v` that gets caught by the regex and
	// replace `-` with `.` to get the version string.
	version = strings.Replace(strings.TrimLeft(rawVersion, "_v"), "-", ".", 2)

	// Put the filename back together without version.
	i := strings.Index(filename, rawVersion)
	if i < 0 {
		// extracted version not in string (impossible)
		return "", "", false
	}
	filename = filename[:i] + filename[i+len(rawVersion):]

	// Put the full path back together and return it.
	// `dirPath + filename` is guaranteed by path.Split()
	return dirPath + filename, version, true
}
