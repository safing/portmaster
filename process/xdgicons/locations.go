package process

// spec: https://specifications.freedesktop.org/icon-theme-spec/icon-theme-spec-latest.html
// alternatives:
// gtk3 lib: https://developer.gnome.org/gtk3/stable/GtkIconTheme.html
// qt lib: ?

var (
	starterLocations = []string{
		"/usr/share/applications",
		"/var/lib/flatpak/exports/share/applications/",
	}

	iconLocations = []string{
		"/usr/share/pixmaps",
	}

	xdgIconLocations = []string{
		"/usr/share/default",
		"/usr/share/gnome",
		"/var/lib/flatpak/exports/share",
		"/usr/local/share",
		"/usr/share",
		"/var/lib/snapd/desktop",
	}

	xdgIconPaths = []string{
		"icons/hicolor/512x512/apps",
		"icons/hicolor/256x256/apps",
		"icons/hicolor/192x192/apps",
		"icons/hicolor/128x128/apps",
		"icons/hicolor/96x96/apps",
		"icons/hicolor/72x72/apps",
		"icons/hicolor/64x64/apps",
		"icons/hicolor/48x48/apps",
	}
)
