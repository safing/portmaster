package binmeta

import (
	"context"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/portmaster/base/api"
)

// ProfileIconStoragePath defines the location where profile icons are stored.
// Must be set before anything else from this package is called.
// Must not be changed once set.
var ProfileIconStoragePath = ""

// ErrIconIgnored is returned when the icon should be ignored.
var ErrIconIgnored = errors.New("icon is ignored")

// GetProfileIcon returns the profile icon with the given ID and extension.
func GetProfileIcon(name string) (data []byte, err error) {
	// Check if enabled.
	if ProfileIconStoragePath == "" {
		return nil, errors.New("api icon storage not configured")
	}

	// Check if icon should be ignored.
	if IgnoreIcon(name) {
		return nil, ErrIconIgnored
	}

	// Build storage path.
	iconPath := filepath.Clean(
		filepath.Join(ProfileIconStoragePath, name),
	)

	iconPath, err = filepath.Abs(iconPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check icon path: %w", err)
	}

	// Do a quick check if we are still within the right directory.
	// This check is not entirely correct, but is sufficient for this use case.
	if filepath.Dir(iconPath) != ProfileIconStoragePath {
		return nil, api.ErrorWithStatus(errors.New("invalid icon"), http.StatusBadRequest)
	}

	return os.ReadFile(iconPath)
}

// UpdateProfileIcon creates or updates the given icon.
func UpdateProfileIcon(data []byte, ext string) (filename string, err error) {
	// Check icon size.
	if len(data) > 1_000_000 {
		return "", errors.New("icon too big")
	}

	// Calculate sha1 sum of icon.
	h := crypto.SHA1.New()
	if _, err := h.Write(data); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))

	// Check if icon should be ignored.
	if IgnoreIcon(sum) {
		return "", ErrIconIgnored
	}

	// Check ext.
	ext = strings.ToLower(ext)
	switch ext {
	case "gif":
	case "jpeg":
		ext = "jpg"
	case "jpg":
	case "png":
	case "svg":
	case "tiff":
	case "webp":
	default:
		return "", errors.New("unsupported icon format")
	}

	// Save to disk.
	filename = sum + "." + ext
	return filename, os.WriteFile(filepath.Join(ProfileIconStoragePath, filename), data, 0o0644) //nolint:gosec
}

// LoadAndSaveIcon loads an icon from disk, updates it in the icon database
// and returns the icon object.
func LoadAndSaveIcon(ctx context.Context, iconPath string) (*Icon, error) {
	// Load icon and save it.
	data, err := os.ReadFile(iconPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read icon %s: %w", iconPath, err)
	}
	filename, err := UpdateProfileIcon(data, filepath.Ext(iconPath))
	if err != nil {
		return nil, fmt.Errorf("failed to import icon %s: %w", iconPath, err)
	}
	return &Icon{
		Type:   IconTypeAPI,
		Value:  filename,
		Source: IconSourceCore,
	}, nil
}

// TODO: Clean up icons regularly.
