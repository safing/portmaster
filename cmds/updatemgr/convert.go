package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/safing/portmaster/service/updates"
)

func convertV1(indexData []byte, baseURL string, lastUpdate time.Time) (*updates.Index, error) {
	// Parse old index.
	oldIndex := make(map[string]string)
	err := json.Unmarshal(indexData, &oldIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse old v1 index: %w", err)
	}

	// Create new index.
	newIndex := &updates.Index{
		Published: lastUpdate,
		Artifacts: make([]*updates.Artifact, 0, len(oldIndex)),
	}

	// Convert all entries.
	if err := convertEntries(newIndex, baseURL, oldIndex); err != nil {
		return nil, err
	}

	return newIndex, nil
}

type IndexV2 struct {
	Channel   string
	Published time.Time
	Releases  map[string]string
}

func convertV2(indexData []byte, baseURL string) (*updates.Index, error) {
	// Parse old index.
	oldIndex := &IndexV2{}
	err := json.Unmarshal(indexData, oldIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse old v2 index: %w", err)
	}

	// Create new index.
	newIndex := &updates.Index{
		Published: oldIndex.Published,
		Artifacts: make([]*updates.Artifact, 0, len(oldIndex.Releases)),
	}

	// Convert all entries.
	if err := convertEntries(newIndex, baseURL, oldIndex.Releases); err != nil {
		return nil, err
	}

	return newIndex, nil
}

func convertEntries(index *updates.Index, baseURL string, entries map[string]string) error {
entries:
	for identifier, version := range entries {
		dir, filename := path.Split(identifier)
		artifactPath := GetVersionedPath(identifier, version)

		// Check if file is to be ignored.
		if scanConfig.IsIgnored(artifactPath) {
			continue entries
		}

		// Get the platform.
		var platform string
		splittedPath := strings.Split(dir, "/")
		if len(splittedPath) >= 1 {
			platform = splittedPath[0]
			if platform == "all" {
				platform = ""
			}
		} else {
			continue entries
		}

		// Create new artifact.
		newArtifact := &updates.Artifact{
			Filename: filename,
			URLs:     []string{baseURL + artifactPath},
			Platform: platform,
			Version:  version,
		}

		// Derive unpack setting.
		unpack, err := scanConfig.UnpackSetting(filename)
		if err != nil {
			return fmt.Errorf("failed to get unpack setting for %s: %w", filename, err)
		}
		newArtifact.Unpack = unpack

		// Add to new index.
		index.Artifacts = append(index.Artifacts, newArtifact)
	}

	return nil
}

// GetVersionedPath combines the identifier and version and returns it as a file path.
func GetVersionedPath(identifier, version string) (versionedPath string) {
	identifierPath, filename := path.Split(identifier)

	// Split the filename where the version should go.
	splittedFilename := strings.SplitN(filename, ".", 2)
	// Replace `.` with `-` for the filename format.
	transformedVersion := strings.Replace(version, ".", "-", 2)

	// Put everything back together and return it.
	versionedPath = identifierPath + splittedFilename[0] + "_v" + transformedVersion
	if len(splittedFilename) > 1 {
		versionedPath += "." + splittedFilename[1]
	}
	return versionedPath
}
