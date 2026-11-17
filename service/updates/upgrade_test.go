package updates

import (
	"errors"
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

var errSimulatedCrossDevice = errors.New("simulated cross-device link")

// failRename simulates a rename that is not possible, e.g. across file systems.
func failRename(oldpath, newpath string) error {
	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errSimulatedCrossDevice}
}

//nolint:paralleltest // Replaces the package-level renameFile.
func TestUpgradeCopyFallbackRemovesSource(t *testing.T) {
	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)

	// A file of the old version that the new version does not have.
	err := os.WriteFile(filepath.Join(installedDir, "zzz-old-only"), []byte("mock:old-only"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	renameFile = failRename
	defer func() { renameFile = os.Rename }()

	err = updater.upgrade(downloader, true)
	if err != nil {
		t.Fatalf("upgrade failed: %s", err)
	}

	// The main directory must hold exactly the new version.
	installed := snapshotDir(t, installedDir)
	for _, name := range append([]string{rollbackTestIndex}, artifactNames(downloader.index)...) {
		if _, ok := installed[name]; !ok {
			t.Errorf("%s not installed", name)
		}
		delete(installed, name)
	}
	for name := range installed {
		t.Errorf("unexpected file %q in main directory after upgrade", name)
	}

	// The old version must be in the purge directory only, the downloads must be gone.
	_, err = os.Stat(filepath.Join(updater.cfg.PurgeDirectory, "zzz-old-only"))
	if err != nil {
		t.Errorf("old file not in purge directory: %s", err)
	}
	for name := range snapshotDir(t, updater.cfg.DownloadDirectory) {
		t.Errorf("unexpected file %q left in download directory", name)
	}
}

//nolint:paralleltest // Replaces the package-level renameFile.
func TestRollbackWithCopyFallback(t *testing.T) {
	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)
	before := snapshotDir(t, installedDir)

	// Make the upgrade fail midway: the last artifact is missing in the download dir.
	last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
	err := os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
	if err != nil {
		t.Fatal(err)
	}

	renameFile = failRename
	defer func() { renameFile = os.Rename }()

	err = updater.upgrade(downloader, true)
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}
	compareSnapshots(t, before, snapshotDir(t, installedDir))
	for name := range snapshotDir(t, updater.cfg.PurgeDirectory) {
		t.Errorf("unexpected file %q left in purge directory after rollback", name)
	}
}
