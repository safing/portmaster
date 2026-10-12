package binmeta

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngBytes is a minimal valid PNG (1x1, transparent).
var pngBytes = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

// writeFile creates the file and its parent directories with the given content, failing the test on error.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

// TestSearchDirectory covers one-directory matching: exact and prefix matches, dotted names, case,
// non-icon files, directories named like icons, and missing versus broken directories.
func TestSearchDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Foo.png"), pngBytes)
	writeFile(t, filepath.Join(dir, "foobar.png"), pngBytes)
	writeFile(t, filepath.Join(dir, "foobaz-longer.svg"), []byte("<svg/>"))
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("x"))
	writeFile(t, filepath.Join(dir, "org.gnome.Calculator.svg"), []byte("<svg/>"))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "foo-dir.png"), 0o755))
	writeFile(t, filepath.Join(dir, "foo-dir.png", "foo-dir.png"), pngBytes)

	// Exact match keeps the directory and the original file name case.
	p, _, err := searchDirectory(dir, "foo", true)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Foo.png"), p)

	// Dotted names are compared whole: only the real extension is stripped.
	p, _, err = searchDirectory(dir, "org.gnome.calculator", true)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "org.gnome.Calculator.svg"), p)
	p, _, err = searchDirectory(dir, "org.gnome", true)
	require.NoError(t, err)
	assert.Equal(t, "", p)

	// Shortest prefix match, with its excess length.
	p, excess, err := searchDirectory(dir, "foob", false)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "foobar.png"), p)
	assert.Equal(t, 2, excess)

	// In exact mode a prefix is not a match.
	p, _, err = searchDirectory(dir, "foob", true)
	require.NoError(t, err)
	assert.Equal(t, "", p)

	// Non-icon files are ignored; a directory named like a matching icon
	// file is skipped, not matched and not descended into.
	p, _, err = searchDirectory(dir, "notes", true)
	require.NoError(t, err)
	assert.Equal(t, "", p)
	p, _, err = searchDirectory(dir, "foo-dir", true)
	require.NoError(t, err)
	assert.Equal(t, "", p)

	// An absolute binary path never matches a file name (the v2.2.3 defect:
	// the full path was compared against file stems).
	p, _, err = searchDirectory(dir, "/usr/bin/foo", true)
	require.NoError(t, err)
	assert.Equal(t, "", p)

	// A missing directory is not an error; a file in place of a directory is.
	p, _, err = searchDirectory(filepath.Join(dir, "missing"), "foo", true)
	require.NoError(t, err)
	assert.Equal(t, "", p)
	_, _, err = searchDirectory(filepath.Join(dir, "notes.txt"), "foo", true)
	assert.Error(t, err)
}

// TestSearchForIconPreference covers the preference order across locations (exact over prefix,
// shortest prefix, earlier location on ties) and first-error retention.
func TestSearchForIconPreference(t *testing.T) {
	// Not parallel: swaps the package-level icon locations.
	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()
	writeFile(t, filepath.Join(first, "foo-helper.png"), pngBytes)
	writeFile(t, filepath.Join(second, "icons/hicolor/48x48/apps", "foo.png"), pngBytes)
	writeFile(t, filepath.Join(second, "icons/hicolor/256x256/apps", "bar-extra.png"), pngBytes)
	writeFile(t, filepath.Join(third, "icons/hicolor/48x48/apps", "bar-x.png"), pngBytes)
	writeFile(t, filepath.Join(second, "icons/hicolor/128x128/apps", "baz-1.png"), pngBytes)
	writeFile(t, filepath.Join(third, "icons/hicolor/48x48/apps", "baz-2.png"), pngBytes)

	saved := iconLocations
	t.Cleanup(func() { iconLocations = saved })
	iconLocations = []IconLocation{
		{Directory: first, Type: FlatDir},
		{Directory: second, Type: XDGIcons},
		{Directory: third, Type: XDGIcons},
	}

	// An exact match in a later location beats a prefix match in an earlier one.
	p, err := searchForIcon("/usr/bin/foo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(second, "icons/hicolor/48x48/apps/foo.png"), p)

	// Among prefix matches the shortest wins, even in a later location.
	p, err = searchForIcon("/opt/x/bar")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(third, "icons/hicolor/48x48/apps/bar-x.png"), p)

	// Equal-length prefix matches: the earlier location wins.
	p, err = searchForIcon("/opt/x/baz")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(second, "icons/hicolor/128x128/apps/baz-1.png"), p)

	// The lookup is by base name, case-insensitively.
	p, err = searchForIcon("/some/where/else/Foo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(second, "icons/hicolor/48x48/apps/foo.png"), p)

	// Unknown binary: nothing, no error.
	p, err = searchForIcon("/usr/bin/nothing-here")
	require.NoError(t, err)
	assert.Equal(t, "", p)

	// A broken location (a file where a directory is expected) does not hide
	// an icon elsewhere, and is reported only when nothing is found.
	broken := filepath.Join(t.TempDir(), "notadir")
	writeFile(t, broken, []byte("x"))
	iconLocations = []IconLocation{
		{Directory: broken, Type: FlatDir},
		{Directory: second, Type: XDGIcons},
	}
	p, err = searchForIcon("/usr/bin/foo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(second, "icons/hicolor/48x48/apps/foo.png"), p)
	p, err = searchForIcon("/usr/bin/nothing-here")
	assert.Error(t, err)
	assert.Equal(t, "", p)

	// With two broken locations the first error is the one reported.
	broken2 := filepath.Join(t.TempDir(), "notadir2")
	writeFile(t, broken2, []byte("x"))
	iconLocations = []IconLocation{
		{Directory: broken, Type: FlatDir},
		{Directory: broken2, Type: FlatDir},
	}
	p, err = searchForIcon("/usr/bin/nothing-here")
	require.Error(t, err)
	assert.Equal(t, "", p)
	assert.Contains(t, err.Error(), broken)
	assert.NotContains(t, err.Error(), broken2)
}

// TestReadIconFile covers the defensive read: format signatures, SVG root element, symlinks,
// the size boundary, FIFOs and directories.
func TestReadIconFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A real PNG is accepted and returned unchanged.
	png := filepath.Join(dir, "ok.png")
	writeFile(t, png, pngBytes)
	data, err := readIconFile(png, "png")
	require.NoError(t, err)
	assert.Equal(t, pngBytes, data)

	// An SVG is recognised by its root element, after an XML prolog.
	svg := filepath.Join(dir, "ok.svg")
	writeFile(t, svg, []byte("<?xml version=\"1.0\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>"))
	_, err = readIconFile(svg, "svg")
	require.NoError(t, err)

	// Content that is not an image of the announced format is rejected, even
	// with the right extension (e.g. a symlink named .png to a secret).
	notPNG := filepath.Join(dir, "secret.png")
	writeFile(t, notPNG, []byte("root:x:0:0:root:/root:/bin/bash\n"))
	_, err = readIconFile(notPNG, "png")
	assert.Error(t, err)
	link := filepath.Join(dir, "link.png")
	require.NoError(t, os.Symlink(notPNG, link))
	_, err = readIconFile(link, "png")
	assert.Error(t, err)

	// A symlink to a valid image is followed (flatpak exports are symlinks).
	goodLink := filepath.Join(dir, "good-link.png")
	require.NoError(t, os.Symlink(png, goodLink))
	data, err = readIconFile(goodLink, "png")
	require.NoError(t, err)
	assert.Equal(t, pngBytes, data)

	// The other raster formats are recognised by their signatures, and the
	// announced format must match the content.
	rasters := map[string][]byte{
		"jpg":  {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'},
		"jpeg": {0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10, 'E', 'x', 'i', 'f'},
		"gif":  []byte("GIF89a\x01\x00\x01\x00"),
		"webp": []byte("RIFF\x1a\x00\x00\x00WEBPVP8 "),
		"tiff": []byte("II*\x00\x08\x00\x00\x00"),
	}
	for ext, content := range rasters {
		path := filepath.Join(dir, "raster."+ext)
		writeFile(t, path, content)
		data, err = readIconFile(path, ext)
		require.NoError(t, err, ext)
		assert.Equal(t, content, data, ext)
		_, err = readIconFile(path, "png")
		assert.Error(t, err, ext+" announced as png")
		_, err = readIconFile(png, ext)
		assert.Error(t, err, "png announced as "+ext)
	}
	writeFile(t, filepath.Join(dir, "mm.tiff"), []byte("MM\x00*\x00\x00\x00\x08"))
	_, err = readIconFile(filepath.Join(dir, "mm.tiff"), "tiff")
	require.NoError(t, err)

	// SVG: the first element must be <svg>; comments and a DOCTYPE before it
	// are fine, even when they push the root past the first few KiB.
	longComment := "<!-- " + strings.Repeat("padding ", 1024) + " -->\n"
	svgCases := map[string]bool{
		"<svg xmlns=\"http://www.w3.org/2000/svg\"/>":                        true,
		"<?xml version=\"1.0\"?>\n<!DOCTYPE svg>\n" + longComment + "<svg/>": true,
		"  \n<svg:svg xmlns:svg=\"http://www.w3.org/2000/svg\"/>":            true,
		"<svgSomethingElse/>":         false,
		"<svg width=123>":             false,
		"<svg width=\"&undefined;\">": false,
		"<private><!-- <svg --><secret>root:x:0:0</secret></private>": false,
		"text before <svg/>":              false,
		"root:x:0:0:root:/root:/bin/bash": false,
		"":                                false,
	}
	i := 0
	for content, ok := range svgCases {
		i++
		path := filepath.Join(dir, fmt.Sprintf("case-%d.svg", i))
		writeFile(t, path, []byte(content))
		_, err = readIconFile(path, "svg")
		if ok {
			assert.NoError(t, err, content)
		} else {
			assert.Error(t, err, content)
		}
	}

	// Size boundary: exactly the limit is accepted, one byte more is rejected
	// without being read in full.
	atLimit := filepath.Join(dir, "at-limit.png")
	writeFile(t, atLimit, append(append([]byte{}, pngBytes...), make([]byte, maxIconFileSize-len(pngBytes))...))
	data, err = readIconFile(atLimit, "png")
	require.NoError(t, err)
	assert.Len(t, data, maxIconFileSize)
	big := filepath.Join(dir, "big.png")
	writeFile(t, big, append(append([]byte{}, pngBytes...), make([]byte, maxIconFileSize+1-len(pngBytes))...))
	_, err = readIconFile(big, "png")
	assert.Error(t, err)

	// A FIFO is refused before any blocking read.
	fifo := filepath.Join(dir, "fifo.png")
	require.NoError(t, syscall.Mkfifo(fifo, 0o644))
	_, err = readIconFile(fifo, "png")
	assert.Error(t, err)

	// Directories are refused.
	_, err = readIconFile(dir, "png")
	assert.Error(t, err)
}

// TestLoadAndSaveIconExtension checks the byte-exact round trip through the icon storage,
// which v2.2.3 failed by passing ".png" where "png" was expected.
func TestLoadAndSaveIconExtension(t *testing.T) {
	// Not parallel: sets the package-level storage path.
	if ProfileIconStoragePath != "" {
		t.Skip("icon storage already configured")
	}
	store := t.TempDir()
	ProfileIconStoragePath = store
	t.Cleanup(func() { ProfileIconStoragePath = "" })

	src := filepath.Join(t.TempDir(), "app.png")
	writeFile(t, src, pngBytes)

	icon, err := LoadAndSaveIcon(context.Background(), src)
	require.NoError(t, err) // v2.2.3 failed here: ".png" was passed where "png" is expected
	require.NotNil(t, icon)
	assert.Equal(t, IconTypeAPI, icon.Type)
	assert.Equal(t, IconSourceCore, icon.Source)
	assert.Equal(t, ".png", filepath.Ext(icon.Value))
	stored, err := os.ReadFile(filepath.Join(store, icon.Value))
	require.NoError(t, err)
	assert.Equal(t, pngBytes, stored)
}

// TestFindIconOnThisSystem runs the real lookup for two common desktop binaries; it depends on
// the host and is skipped in short mode.
func TestFindIconOnThisSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("test depends on linux desktop environment")
	}
	t.Parallel()
	for _, name := range []string{"/usr/bin/evolution", "/usr/bin/nextcloud"} {
		iconPath, err := searchForIcon(name)
		if err != nil {
			t.Error(err)
			continue
		}
		t.Logf("icon for %s: %q", name, iconPath)
	}
}
