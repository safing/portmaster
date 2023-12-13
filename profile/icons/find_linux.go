package icons

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindIcon finds an icon for the given binary name.
// Providing the home directory of the user running the process of that binary can help find an icon.
func FindIcon(ctx context.Context, binName string, homeDir string) (*Icon, error) {
	// Search for icon.
	iconPath, err := search(binName, homeDir)
	if iconPath == "" {
		if err != nil {
			return nil, fmt.Errorf("failed to find icon for %s: %w", binName, err)
		}
		return nil, nil
	}

	return LoadAndSaveIcon(ctx, iconPath)
}

func search(binName string, homeDir string) (iconPath string, err error) {
	binName = strings.ToLower(binName)

	// Search for icon path.
	for _, iconLoc := range iconLocations {
		basePath := iconLoc.GetPath(binName, homeDir)
		if basePath == "" {
			continue
		}

		switch iconLoc.Type {
		case FlatDir:
			iconPath, err = searchDirectory(basePath, binName)
		case XDGIcons:
			iconPath, err = searchXDGIconStructure(basePath, binName)
		}

		if iconPath != "" {
			return
		}
	}
	return
}

func searchXDGIconStructure(baseDirectory string, binName string) (iconPath string, err error) {
	for _, xdgIconDir := range xdgIconPaths {
		directory := filepath.Join(baseDirectory, xdgIconDir)
		iconPath, err = searchDirectory(directory, binName)
		if iconPath != "" {
			return
		}
	}
	return
}

func searchDirectory(directory string, binName string) (iconPath string, err error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read directory %s: %w", directory, err)
	}
	fmt.Println(directory)

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
		case len(iconName) < len(binName):
			// Continue to next.
		case iconName == binName:
			// Exact match, return immediately.
			return filepath.Join(directory, entry.Name()), nil
		case strings.HasPrefix(iconName, binName):
			excessChars := len(iconName) - len(binName)
			if bestMatch == "" || excessChars < bestMatchExcessChars {
				bestMatch = entry.Name()
				bestMatchExcessChars = excessChars
			}
		}
	}

	return bestMatch, nil
}
