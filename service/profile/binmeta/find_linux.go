package binmeta

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetIconAndName returns an icon and name of the given binary path.
// The home directory is accepted for interface parity with the other
// platforms; on Linux core only searches root-owned icon locations (see
// iconLocations), so it is not used here.
// Even if an error is returned, the other return values are valid, if set.
func GetIconAndName(ctx context.Context, binPath string, homeDir string) (icon *Icon, name string, err error) {
	// Derive name from binary.
	name = GenerateBinaryNameFromPath(binPath)

	// Search for icon.
	iconPath, err := searchForIcon(binPath)
	if iconPath == "" {
		if err != nil {
			return nil, name, fmt.Errorf("failed to find icon for %s: %w", binPath, err)
		}
		return nil, name, nil
	}

	// Save icon to internal storage.
	icon, err = LoadAndSaveIcon(ctx, iconPath)
	if err != nil {
		return nil, name, fmt.Errorf("failed to store icon for %s: %w", binPath, err)
	}

	return icon, name, nil
}

// searchForIcon looks for an icon file named after the binary in the known
// icon locations. An exact name match in any location wins over a prefix
// match in any location; among prefix matches the one with the fewest extra
// bytes wins, earlier locations first on ties. When nothing is found, the
// first directory error encountered is returned, so that an unreadable
// location is reported without having hidden an icon elsewhere.
func searchForIcon(binPath string) (iconPath string, err error) {
	// Icon files are named after the binary, not after its path.
	binName := strings.ToLower(filepath.Base(binPath))
	if binName == "" || binName == "." || binName == string(filepath.Separator) {
		return "", nil
	}

	for _, exact := range []bool{true, false} {
		var (
			bestMatch            string
			bestMatchExcessBytes int
		)
		for _, iconLoc := range iconLocations {
			basePath := iconLoc.GetPath(binName)
			if basePath == "" {
				continue
			}

			var directories []string
			switch iconLoc.Type {
			case FlatDir:
				directories = []string{basePath}
			case XDGIcons:
				for _, xdgIconDir := range xdgIconPaths {
					directories = append(directories, filepath.Join(basePath, xdgIconDir))
				}
			}

			for _, directory := range directories {
				match, excess, dirErr := searchDirectory(directory, binName, exact)
				switch {
				case dirErr != nil:
					if err == nil {
						err = dirErr
					}
				case match == "":
					// Nothing here.
				case exact:
					return match, nil
				case bestMatch == "" || excess < bestMatchExcessBytes:
					bestMatch = match
					bestMatchExcessBytes = excess
				}
			}
		}
		if bestMatch != "" {
			return bestMatch, nil
		}
	}

	return "", err
}

// searchDirectory looks in one directory for an icon file whose name without
// extension equals name (exact) or starts with it (prefix). Comparison is on
// lower-cased names; the returned path keeps the real file name. For prefix
// matches it also returns how many bytes longer than name the candidate is.
func searchDirectory(directory string, name string, exact bool) (iconPath string, excessBytes int, err error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", 0, nil
		}
		return "", 0, fmt.Errorf("failed to read directory %s: %w", directory, err)
	}

	var bestMatch string
	for _, entry := range entries {
		// Skip dirs.
		if entry.IsDir() {
			continue
		}

		fileName := strings.ToLower(entry.Name())
		ext := filepath.Ext(fileName)
		if !supportedIconExt(ext) {
			continue
		}
		stem := strings.TrimSuffix(fileName, ext)
		switch {
		case stem == name:
			if exact {
				return filepath.Join(directory, entry.Name()), 0, nil
			}
		case exact:
			// Only exact matches wanted.
		case strings.HasPrefix(stem, name):
			if excess := len(stem) - len(name); bestMatch == "" || excess < excessBytes {
				bestMatch = filepath.Join(directory, entry.Name())
				excessBytes = excess
			}
		}
	}

	return bestMatch, excessBytes, nil
}

// supportedIconExt reports whether the file extension (with its leading dot)
// is an icon format the profile icon storage accepts (see UpdateProfileIcon).
func supportedIconExt(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png", "svg", "jpg", "jpeg", "gif", "webp", "tiff":
		return true
	}
	return false
}
