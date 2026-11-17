package updates

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

//nolint:paralleltest // Replaces the package-level renameFile.
func TestRollbackReportsAndKeepsUnrestoredFile(t *testing.T) {
	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)

	// Give the old file distinct content to tell it apart from the new one.
	const name = "portmaster-core"
	const oldContent = "old:portmaster-core"
	err := os.WriteFile(filepath.Join(installedDir, name), []byte(oldContent), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, installedDir)

	// Make the upgrade fail after the new file was installed: the last artifact is missing.
	last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
	if last == name {
		t.Fatalf("unexpected test index: %s must not be the last artifact", name)
	}
	err = os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
	if err != nil {
		t.Fatal(err)
	}

	// Make the restore of the old file fail: the rename fails and a directory
	// blocks the temporary file of the copy fallback.
	purged := filepath.Join(updater.cfg.PurgeDirectory, name)
	blocker := filepath.Join(installedDir, name+".copy")
	renameFile = func(oldpath, newpath string) error {
		if oldpath == purged {
			err := os.MkdirAll(blocker, 0o700)
			if err != nil {
				return err
			}
			return failRename(oldpath, newpath)
		}
		return os.Rename(oldpath, newpath)
	}
	defer func() { renameFile = os.Rename }()

	err = updater.upgrade(downloader, true)
	if err == nil || !strings.Contains(err.Error(), "including recovery") {
		t.Errorf("expected upgrade to report a failed recovery, got: %v", err)
	}
	checkPurgedFile := func(step string) {
		t.Helper()
		content, err := os.ReadFile(purged)
		if err != nil || string(content) != oldContent {
			t.Errorf("%s: old file not kept in purge directory: %q, %v", step, content, err)
		}
	}
	checkPurgedFile("after failed recovery")

	// All other files must be restored.
	after := snapshotDir(t, installedDir)
	for _, ignore := range []string{name, name + ".copy/"} {
		delete(before, ignore)
		delete(after, ignore)
	}
	compareSnapshots(t, before, after)

	// The next upgrade must not delete the unrestored file while it cannot be restored.
	err = updater.upgrade(downloader, true)
	if err == nil {
		t.Error("expected upgrade to be refused while an old file cannot be restored")
	}
	checkPurgedFile("after refused upgrade")

	// Once the cause is gone, the next upgrade restores the file first.
	renameFile = os.Rename
	err = os.RemoveAll(blocker)
	if err != nil {
		t.Fatal(err)
	}
	_ = updater.upgrade(downloader, true) // Fails: the downloads were consumed.
	content, err := os.ReadFile(filepath.Join(installedDir, name))
	if err != nil || string(content) != oldContent {
		t.Errorf("old file not restored by the next upgrade: %q, %v", content, err)
	}
}
