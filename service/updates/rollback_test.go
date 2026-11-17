package updates

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	rollbackTestName  = "Test"
	rollbackTestIndex = "index.json"

	// rollbackTestExecutable is an executable file of both versions.
	rollbackTestExecutable = "portmaster-core"

	// rollbackTestIgnored is a file of both versions that tests add to Config.Ignore.
	rollbackTestIgnored = "portmaster"
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

func TestRollbackRestoresIgnoredFileReplacedByArtifact(t *testing.T) {
	t.Parallel()

	// "portmaster" is ignored by the upgrade, but the new version has an artifact of that name.
	updater, downloader, installedDir := newRollbackTestUpdater(t, []string{rollbackTestIgnored})
	err := os.WriteFile(filepath.Join(installedDir, rollbackTestIgnored), []byte("ignored:portmaster"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, installedDir)

	// Make the upgrade fail after "portmaster" was installed: the last artifact is missing.
	last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
	if last == rollbackTestIgnored {
		t.Fatal("unexpected test index: portmaster must not be the last artifact")
	}
	err = os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(downloader, true)
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}
	compareSnapshots(t, before, snapshotDir(t, installedDir))
}

func TestUpgradeIgnoredFiles(t *testing.T) {
	t.Parallel()

	// "portmaster" is ignored, but replaced by an artifact; "keep" is ignored and stays.
	updater, downloader, installedDir := newRollbackTestUpdater(t, []string{rollbackTestIgnored, "keep"})
	err := os.WriteFile(filepath.Join(installedDir, rollbackTestIgnored), []byte("ignored:portmaster"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(installedDir, "keep"), []byte("ignored:keep"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(downloader, true)
	if err != nil {
		t.Fatalf("upgrade failed: %s", err)
	}
	installed := snapshotDir(t, installedDir)
	if installed[rollbackTestIgnored] != "mock:portmaster" {
		t.Errorf("artifact portmaster not installed, got %q", installed[rollbackTestIgnored])
	}
	if installed["keep"] != "ignored:keep" {
		t.Errorf("ignored file keep changed, got %q", installed["keep"])
	}
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

func TestRollbackRestoresDirectoryReplacedByArtifact(t *testing.T) {
	t.Parallel()

	// The old version has a directory where the new version has the artifact "portmaster".
	const name = "portmaster"
	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)
	oldDir := filepath.Join(installedDir, name)
	err := os.Remove(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(oldDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(oldDir, "keep"), []byte("old:keep"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, installedDir)

	// Make the upgrade fail after "portmaster" was installed: the last artifact is missing.
	last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
	if last == name {
		t.Fatal("unexpected test index: portmaster must not be the last artifact")
	}
	err = os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(downloader, true)
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}
	if strings.Contains(err.Error(), "including recovery") {
		t.Errorf("expected recovery to succeed, got: %s", err)
	}
	info, err := os.Stat(oldDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o750 {
		t.Fatalf("directory not restored with its permissions: %v, %v", info, err)
	}
	compareSnapshots(t, before, snapshotDir(t, installedDir))
}

// snapshotModes returns the permissions of all files below dir (relative path -> mode).
func snapshotModes(t *testing.T, dir string) map[string]fs.FileMode {
	t.Helper()

	modes := make(map[string]fs.FileMode)
	err := fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		modes[path] = info.Mode().Perm()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return modes
}

//nolint:paralleltest // Replaces the package-level renameFile.
func TestRollbackRestoresFilePermissions(t *testing.T) {
	if onWindows {
		t.Skip("file permissions are ACLs on Windows")
	}

	for _, tc := range []struct {
		name   string
		rename func(oldpath, newpath string) error
	}{
		{name: "rename", rename: os.Rename},
		{name: "copy fallback", rename: failRename},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updater, downloader, installedDir := newRollbackTestUpdater(t, nil)

			// The old version has an executable and a non-executable file.
			for name, mode := range map[string]fs.FileMode{
				rollbackTestExecutable: 0o755,
				"assets.zip":           0o644,
			} {
				err := os.Chmod(filepath.Join(installedDir, name), mode)
				if err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotModes(t, installedDir)

			// Make the upgrade fail midway: the last artifact is missing in the download dir.
			last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
			err := os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
			if err != nil {
				t.Fatal(err)
			}

			renameFile = tc.rename
			defer func() { renameFile = os.Rename }()

			err = updater.upgrade(downloader, true)
			if err == nil || strings.Contains(err.Error(), "including recovery") {
				t.Fatalf("expected upgrade to fail with a successful recovery, got: %v", err)
			}
			after := snapshotModes(t, installedDir)
			for name, mode := range before {
				if after[name] != mode {
					t.Errorf("%s: permissions %v before upgrade, %v after rollback", name, mode, after[name])
				}
			}
		})
	}
}
