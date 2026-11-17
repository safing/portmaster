package updates

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	rollbackTestName  = "Test"
	rollbackTestIndex = "index.json"
)

// snapshotDir returns all files below dir (relative path -> content, directories as empty).
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()

	root := os.DirFS(dir)
	snapshot := make(map[string]string)
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		if d.IsDir() {
			snapshot[path+"/"] = ""
			return nil
		}
		content, err := fs.ReadFile(root, path)
		snapshot[path] = string(content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func compareSnapshots(t *testing.T, before, after map[string]string) {
	t.Helper()

	for name, content := range after {
		beforeContent, ok := before[name]
		switch {
		case !ok:
			t.Errorf("unexpected leftover %q after rollback", name)
		case beforeContent != content:
			t.Errorf("%q changed by rollback", name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			t.Errorf("%q missing after rollback", name)
		}
	}
}

// newRollbackTestUpdater prepares an old version in the installed dir and a new
// version, which has an additional first artifact, in the update dir.
func newRollbackTestUpdater(t *testing.T, ignore []string) (updater *Updater, downloader *Downloader, installedDir string) {
	t.Helper()

	installedDir = t.TempDir()
	updateDir := t.TempDir()
	now := time.Now()

	err := GenerateMockFolder(installedDir, rollbackTestName, "1.0.0", now)
	if err != nil {
		t.Fatal(err)
	}
	err = GenerateMockFolder(updateDir, rollbackTestName, "1.0.1", now.Add(1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	updater, err = New(&testInstance{}, rollbackTestName, Config{
		Name:              rollbackTestName,
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    t.TempDir(),
		IndexFile:         rollbackTestIndex,
		Ignore:            ignore,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add a file that only exists in the new version and sorts before all others.
	err = os.WriteFile(filepath.Join(updateDir, "aaa-new-only"), []byte("mock:new-only"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(updateDir, rollbackTestIndex)) // The index is not an artifact of itself.
	index, err := GenerateIndexFromDir(updateDir, IndexScanConfig{Name: rollbackTestName, Version: "1.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	index.Published = now.Add(1 * time.Minute)
	if len(index.Artifacts) < 3 || index.Artifacts[0].Filename != "aaa-new-only" {
		t.Fatalf("unexpected test index: %+v", index.Artifacts)
	}
	indexJSON, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(updateDir, rollbackTestIndex), indexJSON, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	return updater, &Downloader{u: updater, index: index}, installedDir
}

func TestRollbackRemovesNewOnlyFiles(t *testing.T) {
	t.Parallel()

	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)
	before := snapshotDir(t, installedDir)

	// Make the upgrade fail midway: the last artifact is missing in the download dir.
	last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
	err := os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(downloader, true)
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}
	compareSnapshots(t, before, snapshotDir(t, installedDir))
}

func TestRollbackLeavesNoCopyFiles(t *testing.T) {
	t.Parallel()

	// "portmaster" is ignored by the upgrade, so it stays in place.
	updater, downloader, installedDir := newRollbackTestUpdater(t, []string{"portmaster"})

	// Replace the file with a non-empty directory: neither rename nor the
	// copy fallback can replace it, but the fallback creates "portmaster.copy" first.
	blocker := filepath.Join(installedDir, "portmaster")
	err := os.Remove(blocker)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(blocker, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(blocker, "keep"), []byte("keep"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, installedDir)

	err = updater.upgrade(downloader, true)
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}
	compareSnapshots(t, before, snapshotDir(t, installedDir))
}
