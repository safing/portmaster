package updates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	markerTestOldContent = "old:portmaster-core"
	markerTestOldVersion = "1.0.0"
	markerTestNewVersion = "1.0.1"
)

// failedRollbackState is the state after an upgrade whose rollback could not
// restore the old version of rollbackTestExecutable.
type failedRollbackState struct {
	updater      *Updater
	downloader   *Downloader
	installedDir string
	before       map[string]string // Main directory before the upgrade.
	purged       string            // Unrestored old file in the purge directory.
	blocker      string            // Blocks the restore of purged.
}

// newFailedRollbackState runs an upgrade that fails and whose rollback cannot
// restore one file. It leaves renameFile replaced: the restore of that file
// keeps failing until unblock is called.
func newFailedRollbackState(t *testing.T) *failedRollbackState {
	t.Helper()

	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)
	s := &failedRollbackState{
		updater:      updater,
		downloader:   downloader,
		installedDir: installedDir,
		purged:       filepath.Join(updater.cfg.PurgeDirectory, rollbackTestExecutable),
		blocker:      filepath.Join(installedDir, rollbackTestExecutable+".copy"),
	}

	// Give the old file distinct content to tell it apart from the new one.
	err := os.WriteFile(filepath.Join(installedDir, rollbackTestExecutable), []byte(markerTestOldContent), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	s.before = snapshotDir(t, installedDir)

	// Make the upgrade fail after the new file was installed: the last artifact is missing.
	last := downloader.index.Artifacts[len(downloader.index.Artifacts)-1].Filename
	if last == rollbackTestExecutable {
		t.Fatalf("unexpected test index: %s must not be the last artifact", rollbackTestExecutable)
	}
	err = os.Remove(filepath.Join(updater.cfg.DownloadDirectory, last))
	if err != nil {
		t.Fatal(err)
	}

	// Make the restore of the old file fail: the rename fails and a directory
	// blocks the temporary file of the copy fallback.
	renameFile = func(oldpath, newpath string) error {
		if oldpath == s.purged {
			err := os.MkdirAll(s.blocker, 0o700)
			if err != nil {
				return err
			}
			return failRename(oldpath, newpath)
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = os.Rename })

	err = updater.upgrade(downloader, true)
	if err == nil || !strings.Contains(err.Error(), "including recovery") {
		t.Fatalf("expected upgrade to report a failed recovery, got: %v", err)
	}
	s.checkPurged(t, "after failed rollback")
	return s
}

// unblock makes the restore of the purged file possible again.
func (s *failedRollbackState) unblock(t *testing.T) {
	t.Helper()

	renameFile = os.Rename
	err := os.RemoveAll(s.blocker)
	if err != nil {
		t.Fatal(err)
	}
}

func (s *failedRollbackState) checkPurged(t *testing.T, step string) {
	t.Helper()

	content, err := os.ReadFile(s.purged)
	if err != nil || string(content) != markerTestOldContent {
		t.Fatalf("%s: old file not kept in purge directory: %q, %v", step, content, err)
	}
}

// checkRestored checks that the main directory holds exactly the old version.
func (s *failedRollbackState) checkRestored(t *testing.T) {
	t.Helper()

	compareSnapshots(t, s.before, snapshotDir(t, s.installedDir))
}

// restartUpdater simulates a restart: it returns a new Updater on the same directories.
func restartUpdater(t *testing.T, u *Updater) *Updater {
	t.Helper()

	restarted, err := New(&testInstance{}, u.cfg.Name, u.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return restarted
}

func markerPath(u *Updater) string {
	return filepath.Join(u.cfg.PurgeDirectory, upgradeMarkerName)
}

func checkMarker(t *testing.T, u *Updater, step string, want bool) {
	t.Helper()

	_, err := os.Lstat(markerPath(u))
	if exists := err == nil; exists != want {
		t.Errorf("%s: upgrade marker exists: %v, want %v (%v)", step, exists, want, err)
	}
}

// A restart between a failed rollback and the next upgrade must not lose the
// files that the rollback could not restore.
//
//nolint:paralleltest // Replaces the package-level renameFile.
func TestUpgradeAfterRestartRestoresFilesOfFailedRollback(t *testing.T) {
	s := newFailedRollbackState(t)

	// Restart while the restore still fails: the start-up restore fails too.
	updater := restartUpdater(t, s.updater)
	s.checkPurged(t, "after restart")

	// Once the cause is gone, the next upgrade restores the file first. The
	// upgrade itself fails then (the downloads were consumed) and is rolled back.
	s.unblock(t)
	err := updater.upgrade(s.downloader, true)
	if err == nil || strings.Contains(err.Error(), "including recovery") {
		t.Errorf("expected upgrade to fail with a successful recovery, got: %v", err)
	}
	s.checkRestored(t)
}

// The upgrade is refused while the files of a failed rollback cannot be
// restored, also after a restart, and the purge directory is not touched.
//
//nolint:paralleltest // Replaces the package-level renameFile.
func TestUpgradeRefusedWhileMarkedFilesCannotBeRestored(t *testing.T) {
	s := newFailedRollbackState(t)
	checkMarker(t, s.updater, "after failed rollback", true)

	for _, step := range []string{"without restart", "after restart"} {
		updater := s.updater
		if step == "after restart" {
			updater = restartUpdater(t, s.updater)
		}
		purgeBefore := snapshotDir(t, updater.cfg.PurgeDirectory)
		err := updater.upgrade(s.downloader, true)
		if err == nil || !strings.Contains(err.Error(), "cannot be restored") {
			t.Errorf("%s: expected upgrade to be refused, got: %v", step, err)
		}
		s.checkPurged(t, step)
		checkMarker(t, updater, step, true)

		// Only the file that cannot be restored is left.
		purgeAfter := snapshotDir(t, updater.cfg.PurgeDirectory)
		compareSnapshots(t, purgeBefore, purgeAfter)
		for name := range purgeAfter {
			if name != rollbackTestExecutable && name != upgradeMarkerName {
				t.Errorf("%s: unexpected file %q in purge directory", step, name)
			}
		}
	}
}

// The start-up restores the files of an upgrade that was not committed.
//
//nolint:paralleltest // Replaces the package-level renameFile.
func TestStartRestoresFilesOfFailedRollback(t *testing.T) {
	s := newFailedRollbackState(t)
	s.unblock(t)

	updater := restartUpdater(t, s.updater)
	s.checkRestored(t)
	checkMarker(t, updater, "after start-up restore", false)
	for name := range snapshotDir(t, updater.cfg.PurgeDirectory) {
		t.Errorf("unexpected file %q left in purge directory", name)
	}
	if updater.index == nil || updater.index.Version != markerTestOldVersion {
		t.Errorf("restored index not loaded: %+v", updater.index)
	}

	// The next upgrade works normally: it installs the new version.
	_, downloader, _ := newRollbackTestUpdater(t, nil)
	updater.cfg.DownloadDirectory = downloader.u.cfg.DownloadDirectory
	err := updater.upgrade(downloader, true)
	if err != nil {
		t.Fatalf("upgrade after restore failed: %s", err)
	}
	checkMarker(t, updater, "after upgrade", false)
}

// errSimulatedCrash stops an upgrade like a crash: nothing is rolled back.
type errSimulatedCrash struct{}

// crashUpgrade runs an upgrade that stops (without rollback) when crashAt is moved.
func crashUpgrade(t *testing.T, updater *Updater, downloader *Downloader, crashAt string) {
	t.Helper()

	renameFile = func(oldpath, newpath string) error {
		if oldpath == crashAt {
			panic(errSimulatedCrash{})
		}
		return os.Rename(oldpath, newpath)
	}
	defer func() {
		renameFile = os.Rename
		if r := recover(); r != (errSimulatedCrash{}) {
			t.Fatalf("expected the upgrade to crash, got: %v", r)
		}
	}()
	_ = updater.upgrade(downloader, true)
}

// A restart after a crash during the upgrade restores the installed version.
//
//nolint:paralleltest // Replaces the package-level renameFile.
func TestStartRestoresFilesOfCrashedUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name    string
		crashAt func(u *Updater, d *Downloader) string
		// newOnly is left in the main directory: the restore does not know
		// which files the crashed upgrade installed.
		newOnly bool
	}{
		{
			name: "while purging the installed version",
			crashAt: func(u *Updater, _ *Downloader) string {
				return filepath.Join(u.cfg.Directory, rollbackTestExecutable)
			},
		},
		{
			name: "while installing the new version",
			crashAt: func(u *Updater, d *Downloader) string {
				return filepath.Join(u.cfg.DownloadDirectory, d.index.Artifacts[len(d.index.Artifacts)-1].Filename)
			},
			newOnly: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updater, downloader, installedDir := newRollbackTestUpdater(t, nil)
			before := snapshotDir(t, installedDir)

			crashUpgrade(t, updater, downloader, tc.crashAt(updater, downloader))
			checkMarker(t, updater, "after crash", true)

			restarted := restartUpdater(t, updater)
			after := snapshotDir(t, installedDir)
			if tc.newOnly {
				if _, ok := after["aaa-new-only"]; !ok {
					t.Error("expected the new-only file to be left")
				}
				delete(after, "aaa-new-only")
			}
			compareSnapshots(t, before, after)
			checkMarker(t, restarted, "after restart", false)
			if restarted.index == nil || restarted.index.Version != markerTestOldVersion {
				t.Errorf("restored index not loaded: %+v", restarted.index)
			}
			if restarted.corruptedInstallation != nil {
				t.Errorf("restored installation reported as corrupted: %s", restarted.corruptedInstallation)
			}
		})
	}
}

// The marker is written before the installed version is moved to the purge
// directory and removed when the upgrade is committed: a restart after a
// successful upgrade must not restore (downgrade to) the old version.
//
//nolint:paralleltest // Replaces the package-level renameFile.
func TestUpgradeMarkerLifecycle(t *testing.T) {
	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)

	// The purge directory of a previous successful upgrade is reset.
	err := os.MkdirAll(updater.cfg.PurgeDirectory, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(updater.cfg.PurgeDirectory, "stale")
	err = os.WriteFile(stale, []byte("stale"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	var purgedWithoutMarker []string
	renameFile = func(oldpath, newpath string) error {
		if filepath.Dir(newpath) == updater.cfg.PurgeDirectory {
			_, err := os.Lstat(markerPath(updater))
			if err != nil {
				purgedWithoutMarker = append(purgedWithoutMarker, oldpath)
			}
		}
		return os.Rename(oldpath, newpath)
	}
	defer func() { renameFile = os.Rename }()

	err = updater.upgrade(downloader, true)
	if err != nil {
		t.Fatalf("upgrade failed: %s", err)
	}
	if len(purgedWithoutMarker) > 0 {
		t.Errorf("files moved to the purge directory before the marker was written: %v", purgedWithoutMarker)
	}
	_, err = os.Lstat(stale)
	if err == nil {
		t.Error("purge directory of a previous upgrade not reset")
	}
	checkMarker(t, updater, "after upgrade", false)

	// The old version stays in the purge directory, but is not restored.
	_, err = os.Lstat(filepath.Join(updater.cfg.PurgeDirectory, rollbackTestExecutable))
	if err != nil {
		t.Errorf("old version not in purge directory: %s", err)
	}
	upgraded := snapshotDir(t, installedDir)
	restarted := restartUpdater(t, updater)
	compareSnapshots(t, upgraded, snapshotDir(t, installedDir))
	if restarted.index == nil || restarted.index.Version != markerTestNewVersion {
		t.Errorf("upgraded index not loaded after restart: %+v", restarted.index)
	}
}

// The marker is never restored into the main directory, never treated as a
// purged file or as an existing artifact, and cannot be an artifact.
//
//nolint:paralleltest // Replaces the package-level renameFile.
func TestUpgradeMarkerIsNotAFile(t *testing.T) {
	s := newFailedRollbackState(t)
	checkMarker(t, s.updater, "after failed rollback", true)

	// The marker is not an existing file that an artifact could be copied from.
	d := &Downloader{u: s.updater}
	err := d.gatherExistingFiles(s.updater.cfg.PurgeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range d.existingFiles {
		if isUpgradeMarker(filepath.Base(path)) {
			t.Errorf("marker gathered as existing file: %s", path)
		}
	}
	if len(d.existingFiles) != 1 {
		t.Errorf("expected only the purged file to be gathered, got %v", d.existingFiles)
	}

	// The marker is not restored.
	s.unblock(t)
	err = s.updater.recoverFromFailedUpgrade(nil)
	if err != nil {
		t.Fatalf("restore failed: %s", err)
	}
	s.checkRestored(t)
	checkMarker(t, s.updater, "after restore", false)

	// An artifact cannot have the name of the marker.
	for _, name := range []string{upgradeMarkerName, upgradeMarkerName + ".tmp", strings.ToUpper(upgradeMarkerName)} {
		if validateArtifactFilename(name) == nil {
			t.Errorf("artifact filename %q accepted", name)
		}
	}
}

// A file with the name of the marker in the main directory is not moved to
// the purge directory, where it would replace the marker.
func TestUpgradeKeepsMarkerNamedFileInMainDirectory(t *testing.T) {
	t.Parallel()

	updater, downloader, installedDir := newRollbackTestUpdater(t, nil)
	stray := filepath.Join(installedDir, upgradeMarkerName)
	err := os.WriteFile(stray, []byte("stray"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(downloader, true)
	if err != nil {
		t.Fatalf("upgrade failed: %s", err)
	}
	content, err := os.ReadFile(stray)
	if err != nil || string(content) != "stray" {
		t.Errorf("file with the marker name not kept in main directory: %q, %v", content, err)
	}
	checkMarker(t, updater, "after upgrade", false)
}

// A file in place of the purge directory has no marker and is replaced as before.
func TestUpgradeReplacesFileAtPurgeDirectory(t *testing.T) {
	t.Parallel()

	updater, downloader, _ := newRollbackTestUpdater(t, nil)
	err := os.RemoveAll(updater.cfg.PurgeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(updater.cfg.PurgeDirectory, []byte("file"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	err = updater.upgrade(downloader, true)
	if err != nil {
		t.Fatalf("upgrade failed: %s", err)
	}
	checkMarker(t, updater, "after upgrade", false)
}
