package profile

import (
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/safing/portbase/api"
)

var profileIconStoragePath = ""

// GetProfileIcon returns the profile icon with the given ID and extension.
func GetProfileIcon(name string) (data []byte, err error) {
	// Check if enabled.
	if profileIconStoragePath == "" {
		return nil, errors.New("api icon storage not configured")
	}

	// Build storage path.
	iconPath := filepath.Clean(
		filepath.Join(profileIconStoragePath, name),
	)

	iconPath, err = filepath.Abs(iconPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check icon path: %w", err)
	}

	// Do a quick check if we are still within the right directory.
	// This check is not entirely correct, but is sufficient for this use case.
	if filepath.Dir(iconPath) != profileIconStoragePath {
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
	return filename, os.WriteFile(filepath.Join(profileIconStoragePath, filename), data, 0o0644) //nolint:gosec
}

// TODO: Clean up icons regularly.
