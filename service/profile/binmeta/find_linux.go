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
// Providing the home directory of the user running the process of that binary can improve results.
// Even if an error is returned, the other return values are valid, if set.
func GetIconAndName(ctx context.Context, binPath string, homeDir string) (icon *Icon, name string, err error) {
	// Derive name from binary.
	name = GenerateBinaryNameFromPath(binPath)

	// Search for icon.
	iconPath, err := searchForIcon(binPath, homeDir)
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

func searchForIcon(binPath string, homeDir string) (iconPath string, err error) {
	binPath = strings.ToLower(binPath)

	// Search for icon path.
	for _, iconLoc := range iconLocations {
		basePath := iconLoc.GetPath(binPath, homeDir)
		if basePath == "" {
			continue
		}

		switch iconLoc.Type {
		case FlatDir:
			iconPath, err = searchDirectory(basePath, binPath)
		case XDGIcons:
			iconPath, err = searchXDGIconStructure(basePath, binPath)
		}

		if iconPath != "" {
			return
		}
	}
	return
}

func searchXDGIconStructure(baseDirectory string, binPath string) (iconPath string, err error) {
	for _, xdgIconDir := range xdgIconPaths {
		directory := filepath.Join(baseDirectory, xdgIconDir)
		iconPath, err = searchDirectory(directory, binPath)
		if iconPath != "" {
			return
		}
	}
	return
}

func searchDirectory(directory string, binPath string) (iconPath string, err error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read directory %s: %w", directory, err)
	}
	// DEBUG:
	// fmt.Println(directory)

	var (
		bestMatch            string
		bestMatchExcessChars int
	)
	for _, entry := range entries {
		// Skip dirs.
		if entry.IsDir() {
			continue
		}

		iconName := strings.ToLower(entry.Name())
		iconName = strings.TrimSuffix(iconName, filepath.Ext(iconName))
		switch {
		case len(iconName) < len(binPath):
			// Continue to next.
		case iconName == binPath:
			// Exact match, return immediately.
			return filepath.Join(directory, entry.Name()), nil
		case strings.HasPrefix(iconName, binPath):
			excessChars := len(iconName) - len(binPath)
			if bestMatch == "" || excessChars < bestMatchExcessChars {
				bestMatch = entry.Name()
				bestMatchExcessChars = excessChars
			}
		}
	}

	return bestMatch, nil
}
