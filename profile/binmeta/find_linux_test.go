package binmeta

import (
	"os"
	"testing"
)

func TestFindIcon(t *testing.T) {
	if testing.Short() {
		t.Skip("test depends on linux desktop environment")
	}
	t.Parallel()

	home := os.Getenv("HOME")
	testFindIcon(t, "evolution", home)
	testFindIcon(t, "nextcloud", home)
}

func testFindIcon(t *testing.T, binName string, homeDir string) {
	t.Helper()

	iconPath, err := searchForIcon(binName, homeDir)
	if err != nil {
		t.Error(err)
		return
	}
	if iconPath == "" {
		t.Errorf("no icon found for %s", binName)
		return
	}
	t.Logf("icon for %s found: %s", binName, iconPath)
}
