package binmeta

import (
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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

// maxIconFileSize bounds what readIconFile accepts; it matches the limit
// UpdateProfileIcon enforces on stored icons.
const maxIconFileSize = 1_000_000

// readIconFile reads an icon file for storage. Core runs with elevated
// privileges, so the read is defensive: the file is opened without blocking
// (a FIFO would otherwise stall core until a writer appears), only a regular
// file is accepted on the open handle (no FIFO, device or directory), at most
// maxIconFileSize bytes are read, and the bytes must carry the signature of
// the format the extension announces.
//
// Trust assumption: the search locations (see iconLocations) are
// administrator-controlled trees. Neither directory ownership nor the
// ownership of a symlink target is checked here; symlinks are followed on
// purpose, because flatpak exports are symlink farms. The signature check
// rejects content without the expected signature, it is not a substitute for
// that assumption.
func readIconFile(path string, ext string) ([]byte, error) {
	f, err := openIconFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > maxIconFileSize {
		return nil, errors.New("icon too big")
	}

	// Read the limit plus one probe byte, so that a file growing after Stat
	// is still caught without reading it in full.
	data, err := io.ReadAll(io.LimitReader(f, maxIconFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxIconFileSize {
		return nil, errors.New("icon too big")
	}
	if !looksLikeImage(data, ext) {
		return nil, errors.New("file content does not match the icon format")
	}
	return data, nil
}

// looksLikeImage sniffs the signature of the supported icon formats so that
// a file is only stored (and later served) if it starts like an image of the
// announced kind. For the raster formats this checks the leading magic bytes
// only; it does not prove the rest is a valid image. For SVG the data must
// parse as XML up to its first element, and that element must be <svg>.
func looksLikeImage(data []byte, ext string) bool {
	switch strings.ToLower(ext) {
	case "png":
		return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	case "jpg", "jpeg":
		return bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF})
	case "gif":
		return bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))
	case "webp":
		return len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	case "tiff":
		return bytes.HasPrefix(data, []byte("II*\x00")) || bytes.HasPrefix(data, []byte("MM\x00*"))
	case "svg":
		return isSVGRoot(data)
	default:
		return false
	}
}

// isSVGRoot reports whether the first XML element in data is <svg>, using
// the strict decoder. Comments, processing instructions, a DOCTYPE and
// whitespace before it are allowed; anything that is not well-formed XML up
// to and including that element (unquoted attribute values, unknown
// entities) is not.
func isSVGRoot(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return t.Name.Local == "svg"
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return false
			}
		}
	}
}

// LoadAndSaveIcon loads an icon from disk, updates it in the icon database
// and returns the icon object.
func LoadAndSaveIcon(ctx context.Context, iconPath string) (*Icon, error) {
	// Load icon and save it.
	ext := strings.TrimPrefix(filepath.Ext(iconPath), ".")
	data, err := readIconFile(iconPath, ext)
	if err != nil {
		return nil, fmt.Errorf("failed to read icon %s: %w", iconPath, err)
	}
	filename, err := UpdateProfileIcon(data, ext)
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
