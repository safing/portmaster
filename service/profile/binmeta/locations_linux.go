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
	BinName
)

var (
	// iconLocations are searched by core, which runs with elevated
	// privileges. Only root-owned trees are listed on purpose: an icon found
	// here is read and stored by core, and a user-writable location would let
	// an unprivileged user choose what core reads. Icons of applications
	// installed per user (~/.local/share, ~/.local/share/flatpak) are resolved
	// by the user interface, which runs as that user.
	iconLocations = []IconLocation{
		{Directory: "/usr/share/pixmaps", Type: FlatDir},
		{Directory: "/usr/share", Type: XDGIcons},
		{Directory: "/usr/local/share", Type: XDGIcons},
		{Directory: "/var/lib/flatpak/exports/share", Type: XDGIcons},
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
		"icons/hicolor/scalable/apps",
	}
)

// GetPath returns the base path of an icon location. The binName argument is
// the lower-cased base name of the binary (no directory).
func (il IconLocation) GetPath(binName string) string {
	switch il.PathArg {
	case NoPathArg:
		return il.Directory
	case BinName:
		if binName != "" {
			return fmt.Sprintf(il.Directory, binName)
		}
	}
	return ""
}
