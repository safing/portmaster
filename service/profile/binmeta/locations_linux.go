package binmeta

import (
	"fmt"
)

// IconLocation describes an icon location.
type IconLocation struct {
	Directory string
	Type      IconLocationType
	PathArg   PathArg
}

// IconLocationType describes an icon location type.
type IconLocationType uint8

// Icon Location Types.
const (
	FlatDir IconLocationType = iota
	XDGIcons
)

// PathArg describes an icon location path argument.
type PathArg uint8

// Path Args.
const (
	NoPathArg PathArg = iota
	Home
	BinName
)

var (
	iconLocations = []IconLocation{
		{Directory: "/usr/share/pixmaps", Type: FlatDir},
		{Directory: "/usr/share", Type: XDGIcons},
		{Directory: "%s/.local/share", Type: XDGIcons, PathArg: Home},
		{Directory: "%s/.local/share/flatpak/exports/share", Type: XDGIcons, PathArg: Home},
		{Directory: "/usr/share/%s", Type: XDGIcons, PathArg: BinName},
	}

	xdgIconPaths = []string{
		// UI currently uses 48x48, so 256x256 should suffice for the future, even at 2x. (12.2023)
		"icons/hicolor/256x256/apps",
		"icons/hicolor/192x192/apps",
		"icons/hicolor/128x128/apps",
		"icons/hicolor/96x96/apps",
		"icons/hicolor/72x72/apps",
		"icons/hicolor/64x64/apps",
		"icons/hicolor/48x48/apps",
		"icons/hicolor/512x512/apps",
	}
)

// GetPath returns the path of an icon.
func (il IconLocation) GetPath(binName string, homeDir string) string {
	switch il.PathArg {
	case NoPathArg:
		return il.Directory
	case Home:
		if homeDir != "" {
			return fmt.Sprintf(il.Directory, homeDir)
		}
	case BinName:
		return fmt.Sprintf(il.Directory, binName)
	}
	return ""
}
