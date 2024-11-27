package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	baseIndexExtension = ".json"
	v2IndexExtension   = ".v2.json"
)

// Index describes an index file pulled by the updater.
type Index struct {
	// Path is the path to the index file
	// on the update server.
	Path string

	// Channel holds the release channel name of the index.
	// It must match the filename without extension.
	Channel string

	// PreRelease signifies that all versions of this index should be marked as
	// pre-releases, no matter if the versions actually have a pre-release tag or
	// not.
	PreRelease bool

	// AutoDownload specifies whether new versions should be automatically downloaded.
	AutoDownload bool

	// LastRelease holds the time of the last seen release of this index.
	LastRelease time.Time
}

// IndexFile represents an index file.
type IndexFile struct {
	Channel   string
	Published time.Time

	Releases map[string]string
}

var (
	// ErrIndexChecksumMismatch is returned when an index does not match its
	// signed checksum.
	ErrIndexChecksumMismatch = errors.New("index checksum does mot match signature")

	// ErrIndexFromFuture is returned when an index is parsed with a
	// Published timestamp that lies in the future.
	ErrIndexFromFuture = errors.New("index is from the future")

	// ErrIndexIsOlder is returned when an index is parsed with an older
	// Published timestamp than the current Published timestamp.
	ErrIndexIsOlder = errors.New("index is older than the current one")

	// ErrIndexChannelMismatch is returned when an index is parsed with a
	// different channel that the expected one.
	ErrIndexChannelMismatch = errors.New("index does not match the expected channel")
)

// ParseIndexFile parses an index file and checks if it is valid.
func ParseIndexFile(indexData []byte, channel string, lastIndexRelease time.Time) (*IndexFile, error) {
	// Load into struct.
	indexFile := &IndexFile{}
	err := json.Unmarshal(indexData, indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signed index data: %w", err)
	}

	// Fallback to old format if there are no releases and no channel is defined.
	// TODO: Remove in v1
	if len(indexFile.Releases) == 0 && indexFile.Channel == "" {
		return loadOldIndexFormat(indexData, channel)
	}

	// Check the index metadata.
	switch {
	case !indexFile.Published.IsZero() && time.Now().Before(indexFile.Published):
		return indexFile, ErrIndexFromFuture

	case !indexFile.Published.IsZero() &&
		!lastIndexRelease.IsZero() &&
		lastIndexRelease.After(indexFile.Published):
		return indexFile, ErrIndexIsOlder

	case channel != "" &&
		indexFile.Channel != "" &&
		channel != indexFile.Channel:
		return indexFile, ErrIndexChannelMismatch
	}

	return indexFile, nil
}

func loadOldIndexFormat(indexData []byte, channel string) (*IndexFile, error) {
	releases := make(map[string]string)
	err := json.Unmarshal(indexData, &releases)
	if err != nil {
		return nil, err
	}

	return &IndexFile{
		Channel: channel,
		// Do NOT define `Published`, as this would break the "is newer" check.
		Releases: releases,
	}, nil
}
