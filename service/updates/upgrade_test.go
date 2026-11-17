package updates

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpgradeCreatesMissingMainDirectory(t *testing.T) {
	t.Parallel()

	installedDir := filepath.Join(t.TempDir(), "installed")
	updateDir := t.TempDir()
	err := GenerateMockFolder(updateDir, rollbackTestName, "1.0.1", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	updater, err := New(&testInstance{}, rollbackTestName, Config{
		Name:              rollbackTestName,
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    t.TempDir(),
		IndexFile:         rollbackTestIndex,
	})
	if err != nil {
		t.Fatal(err)
	}
	index, err := LoadIndex(filepath.Join(updateDir, rollbackTestIndex), currentPlatform, nil)
	if err != nil {
		t.Fatal(err)
	}

	// New creates the main directory, but it may be gone by the time of the upgrade.
	err = os.RemoveAll(installedDir)
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(&Downloader{u: updater, index: index}, true)
	if err != nil {
		t.Fatalf("upgrade into missing main directory failed: %s", err)
	}
	for _, name := range append([]string{rollbackTestIndex}, artifactNames(index)...) {
		_, err := os.Stat(filepath.Join(installedDir, name))
		if err != nil {
			t.Errorf("%s not installed: %s", name, err)
		}
	}
}

func artifactNames(index *Index) []string {
	names := make([]string, 0, len(index.Artifacts))
	for _, artifact := range index.Artifacts {
		names = append(names, artifact.Filename)
	}
	return names
}
