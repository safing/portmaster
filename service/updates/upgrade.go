package updates

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

// checkDirWritable checks that files can be created in dir.
func checkDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

func (u *Updater) upgrade(downloader *Downloader, ignoreVersion bool) error {
	// Lock index for the upgrade.
	u.indexLock.Lock()
	defer u.indexLock.Unlock()

	// Check if we should upgrade at all.
	if !ignoreVersion && u.index != nil {
		if err := u.index.ShouldUpgradeTo(downloader.index); err != nil {
			return fmt.Errorf("cannot upgrade: %w", ErrNoUpdateAvailable)
		}
	}

	// If we are running in a UI instance, we need to unload the UI assets
	if u.instance != nil {
		u.instance.UI().EnableUpgradeLock()
		defer u.instance.UI().DisableUpgradeLock()
	}

	// A previous upgrade was not committed (its rollback failed, or the process
	// stopped during it): files of the installed version are still in the purge
	// directory, which the upgrade resets. Restore them first.
	if u.hasUpgradeMarker() {
		err := u.recoverFromFailedUpgrade(nil)
		if err != nil {
			return fmt.Errorf("cannot upgrade: files of a failed upgrade in %s cannot be restored: %w", u.cfg.PurgeDirectory, err)
		}
		// The restored index is the active one now.
		index, err := LoadIndex(filepath.Join(u.cfg.Directory, u.cfg.IndexFile), u.cfg.Platform, u.cfg.Verify)
		if err == nil {
			u.index = index
		}
	}

	// Execute the upgrade.
	previousIndex := u.index
	installedFiles, upgradeError := u.upgradeMoveFiles(downloader)
	if upgradeError == nil {
		// Files upgraded successfully.
		// Applying post-upgrade tasks, if any.
		upgradeError = u.applyPostUpgradeCommands()
	}

	if upgradeError == nil {
		// Commit the upgrade: from now on, the purge directory holds only the
		// previous version, which must not be restored anymore.
		upgradeError = u.removeUpgradeMarker()
		if upgradeError == nil {
			return nil
		}
	}

	// Attempt to recover from failed upgrade.
	// The recovery moves the previous files (including the index file) back,
	// so the previous index must be active again too.
	u.index = previousIndex
	recoveryErr := u.recoverFromFailedUpgrade(installedFiles)
	if recoveryErr == nil {
		return fmt.Errorf("upgrade failed, but recovery was successful: %w", upgradeError)
	}

	// Recovery failed too. The upgrade marker stays, so that the files are
	// restored on the next start or before the next upgrade.
	return fmt.Errorf("upgrade (including recovery) failed: %w (recovery: %w)", upgradeError, recoveryErr)
}

// upgradeMoveFiles moves the new version into place. It returns the names of the
// new version's files that were moved into the main directory, also on failure.
func (u *Updater) upgradeMoveFiles(downloader *Downloader) (installedFiles []string, err error) {
	// Important:
	// We assume that the downloader has done its job and all artifacts are verified.
	// Files will just be moved here.
	// In case the files are copied, they are verified in the process.

	// Reset purge directory, so that we can do a clean rollback later.
	_ = os.RemoveAll(u.cfg.PurgeDirectory)
	err = utils.EnsureDirectory(u.cfg.PurgeDirectory, utils.PublicReadExecPermission)
	if err != nil {
		return nil, fmt.Errorf("failed to create purge directory: %w", err)
	}
	// Mark the purge directory before it receives files of the installed version.
	err = u.writeUpgradeMarker()
	if err != nil {
		return nil, err
	}

	// Move current version files into purge folder.
	if u.index != nil {
		log.Debugf("updates/%s: removing the old version (v%s from %s)", u.cfg.Name, u.index.Version, u.index.Published)
	}
	files, err := os.ReadDir(u.cfg.Directory)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read current directory: %w", err)
		}
		err := utils.EnsureDirectory(u.cfg.Directory, utils.PublicReadExecPermission)
		if err != nil {
			return nil, fmt.Errorf("create current directory: %w", err)
		}
	} else {
		// Names the new version installs into the main directory.
		newFiles := make(map[string]struct{}, len(downloader.index.Artifacts)+1)
		newFiles[u.cfg.IndexFile] = struct{}{}
		for _, artifact := range downloader.index.Artifacts {
			newFiles[artifact.Filename] = struct{}{}
		}

		// Move files.
		for _, file := range files {
			// Check if file is ignored.
			// An ignored file that a new file replaces is moved to the purge dir
			// too, so that a rollback can restore it.
			if slices.Contains(u.cfg.Ignore, file.Name()) {
				if _, replaced := newFiles[file.Name()]; !replaced || file.IsDir() {
					continue
				}
			}
			// A file with the name of the marker is no artifact (see
			// validateArtifactFilename) and would replace the marker.
			if isUpgradeMarker(file.Name()) {
				continue
			}
			// ignore PurgeDirectory itself
			if strings.EqualFold(u.cfg.PurgeDirectory, filepath.Join(u.cfg.Directory, file.Name())) {
				continue
			}

			// Otherwise, move file to purge dir.
			src := filepath.Join(u.cfg.Directory, file.Name())
			dst := filepath.Join(u.cfg.PurgeDirectory, file.Name())
			err := u.moveInstalledFile(src, dst)
			if err != nil {
				return nil, fmt.Errorf("failed to move current file %s to purge dir: %w", file.Name(), err)
			}
		}
	}

	// Move the new index file into main directory.
	log.Debugf("updates/%s: installing the new version (v%s from %s)", u.cfg.Name, downloader.index.Version, downloader.index.Published)
	src := filepath.Join(u.cfg.DownloadDirectory, u.cfg.IndexFile)
	dst := filepath.Join(u.cfg.Directory, u.cfg.IndexFile)
	err = u.moveFile(src, dst, "", utils.PublicReadPermission)
	if err != nil {
		return nil, fmt.Errorf("failed to move index file to %s: %w", dst, err)
	}
	installedFiles = append(installedFiles, u.cfg.IndexFile)

	// Move downloaded files to the current version folder.
	for _, artifact := range downloader.index.Artifacts {
		src = filepath.Join(u.cfg.DownloadDirectory, artifact.Filename)
		dst = filepath.Join(u.cfg.Directory, artifact.Filename)
		err = u.moveFile(src, dst, artifact.SHA256, artifact.GetFileMode())
		if err != nil {
			return installedFiles, fmt.Errorf("failed to move file %s: %w", artifact.Filename, err)
		} else {
			installedFiles = append(installedFiles, artifact.Filename)
			log.Debugf("updates/%s: %s moved", u.cfg.Name, artifact.Filename)
		}
	}

	// Set new index on module.
	u.index = downloader.index
	log.Infof("updates/%s: update complete (v%s from %s)", u.cfg.Name, u.index.Version, u.index.Published)

	return installedFiles, nil
}

// onWindows is true if file permissions are ACLs instead of Unix modes.
const onWindows = runtime.GOOS == "windows"

// renameFile is os.Rename. Tests replace it to exercise the copy fallback of moveFile.
var renameFile = os.Rename

// moveFile moves a file and falls back to copying if it fails.
// The moved file gets filePermission.
func (u *Updater) moveFile(currentPath, newPath string, sha256sum string, filePermission utils.FSPermission) error {
	return u.moveFileWithPermission(currentPath, newPath, sha256sum, filePermission, false)
}

// moveInstalledFile moves a file of the installed version between the main and
// the purge directory. On Unix, the file keeps its permissions, so that a
// rollback restores them: e.g. an executable must stay executable.
// On Windows, it gets PublicReadPermission like before, which grants the same
// ACLs as PublicReadExecPermission there.
func (u *Updater) moveInstalledFile(currentPath, newPath string) error {
	return u.moveFileWithPermission(currentPath, newPath, "", utils.PublicReadPermission, !onWindows)
}

func (u *Updater) moveFileWithPermission(currentPath, newPath string, sha256sum string, filePermission utils.FSPermission, keepUnixMode bool) error {
	// Try to simply move file.
	err := renameFile(currentPath, newPath)
	if err == nil {
		// Moving was successful, return.
		if keepUnixMode {
			// The file kept its permissions.
			return nil
		}
		// A moved directory (of the old version) keeps its permissions: file
		// permissions would make its contents inaccessible.
		info, statErr := os.Lstat(newPath)
		if statErr != nil || !info.IsDir() {
			_ = utils.SetFilePermission(newPath, filePermission)
		}
		return nil
	}
	log.Tracef("updates/%s: failed to move to %q, falling back to copy+delete: %s", u.cfg.Name, newPath, err)

	// The copy must get the permissions of the source.
	var srcInfo os.FileInfo
	if keepUnixMode {
		srcInfo, err = os.Stat(currentPath)
		if err != nil {
			return fmt.Errorf("move failed, copy+delete fallback failed: %w", err)
		}
	}

	// Copy and check the checksum while we are at it.
	err = copyAndCheckSHA256Sum(currentPath, newPath, sha256sum, filePermission)
	if err != nil {
		return fmt.Errorf("move failed, copy+delete fallback failed: %w", err)
	}
	if keepUnixMode {
		err = os.Chmod(newPath, srcInfo.Mode().Perm())
		if err != nil {
			// The source is still in place: remove the copy and keep the source.
			_ = os.Remove(newPath)
			return fmt.Errorf("move failed, copy+delete fallback failed to set permissions: %w", err)
		}
	}

	// Complete the move: the source must not remain, e.g. an old file in the main directory.
	// The copy is in place, so a source that cannot be deleted (e.g. a file in
	// use on Windows) does not fail the move. It is left behind as before.
	err = os.Remove(currentPath)
	if err != nil {
		log.Warningf("updates/%s: copied %q to %q, but failed to delete the source: %s", u.cfg.Name, currentPath, newPath, err)
	}

	return nil
}

// recoverFromFailedUpgrade attempts to roll back any moved files by the upgrade process.
// installedFiles are the names of the new version's files that the upgrade moved
// into the main directory.
func (u *Updater) recoverFromFailedUpgrade(installedFiles []string) error {
	// Get list of files from purge dir.
	files, err := os.ReadDir(u.cfg.PurgeDirectory)
	if err != nil {
		return err
	}
	files = slices.DeleteFunc(files, func(file os.DirEntry) bool {
		return isUpgradeMarker(file.Name())
	})

	// Remember which files had an old counterpart.
	purged := make(map[string]struct{}, len(files))
	for _, file := range files {
		purged[file.Name()] = struct{}{}
	}

	// Move all files back to main dir.
	var restoreErrs []error
	for _, file := range files {
		purgedFile := filepath.Join(u.cfg.PurgeDirectory, file.Name())
		activeFile := filepath.Join(u.cfg.Directory, file.Name())
		if file.IsDir() {
			// A directory cannot be moved onto a file. The old version had this
			// directory in the main directory, so a file in its place was
			// installed by the upgrade: remove it.
			info, err := os.Lstat(activeFile)
			if err == nil && !info.IsDir() {
				err = os.Remove(activeFile)
				if err != nil {
					log.Warningf("updates/%s: failed to remove new file %s while rolling back: %s", u.cfg.Name, file.Name(), err)
				}
			}
		}
		err := u.moveInstalledFile(purgedFile, activeFile)
		if err != nil {
			// Continue to recover as many files as possible, but report the
			// failure: the file is only in the purge directory now.
			log.Warningf("updates/%s: failed to roll back file %s: %s", u.cfg.Name, file.Name(), err)
			restoreErrs = append(restoreErrs, fmt.Errorf("roll back %s: %w", file.Name(), err))
		}
	}

	// Remove files of the new version that had no counterpart in the old
	// version: moving the old files back does not replace them.
	// Only files that this upgrade installed are removed, never anything that
	// was purged (it was restored above) or is ignored by the upgrade.
	for _, name := range installedFiles {
		if _, ok := purged[name]; ok || slices.Contains(u.cfg.Ignore, name) {
			continue
		}
		err := os.Remove(filepath.Join(u.cfg.Directory, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warningf("updates/%s: failed to remove new file %s while rolling back: %s", u.cfg.Name, name, err)
		}
	}

	if len(restoreErrs) > 0 {
		return errors.Join(restoreErrs...)
	}

	// All files of the installed version are back in the main directory.
	return u.removeUpgradeMarker()
}

// upgradeMarkerName is the name of the upgrade marker in the purge directory.
// The marker exists while the upgrade is not committed: from before the
// installed version is moved to the purge directory until the new version is
// completely installed, or until the installed version is completely
// restored. While it exists, the purge directory holds files of the installed
// version: they are restored on start and before the next upgrade, instead of
// being deleted. It survives a restart, unlike state in memory.
//
// Phases of an upgrade and what the marker means for them:
//   - Crash while moving the installed version to the purge directory: the
//     restore moves the purged files back, the installed version is complete.
//   - Crash while moving the new version in: the restore moves the installed
//     version back over the new files. Files that only the new version has are
//     left in the main directory, as the installed files are not recorded.
//   - Failed rollback: the marker stays and the restore is retried; until it
//     succeeds, upgrades are refused and the purge directory is kept.
//   - Successful upgrade: the marker is removed as the last step of the
//     upgrade, after the post-upgrade commands, so a restart afterwards does
//     not restore (downgrade to) the previous version that stays in the purge
//     directory. A crash right before the removal leaves the marker: the
//     upgrade is then not committed and the restart rolls it back, which
//     restores the complete previous version. If the marker cannot be
//     removed, the upgrade fails and is rolled back.
//
// The name and its prefix are rejected as artifact filenames, so the marker
// cannot collide with an artifact.
const upgradeMarkerName = "portmaster-upgrade-uncommitted.marker"

// isUpgradeMarker reports whether name is the upgrade marker or its temporary file.
func isUpgradeMarker(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), upgradeMarkerName)
}

func (u *Updater) upgradeMarkerPath() string {
	return filepath.Join(u.cfg.PurgeDirectory, upgradeMarkerName)
}

// hasUpgradeMarker reports whether the purge directory holds files of an
// upgrade that was not committed. If that cannot be determined, it reports
// true, so that the files are not deleted. A purge directory that does not
// exist or is not a directory has no marker.
func (u *Updater) hasUpgradeMarker() bool {
	_, err := os.Lstat(u.upgradeMarkerPath())
	return !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR)
}

// writeUpgradeMarker atomically creates the upgrade marker.
func (u *Updater) writeUpgradeMarker() error {
	path := u.upgradeMarkerPath()
	tmpPath := path + ".tmp"
	content := "The upgrade of " + u.cfg.Name + " is not committed: the files in this directory belong to the installed version and are restored on the next start or upgrade.\n"
	err := os.WriteFile(tmpPath, []byte(content), 0o600)
	if err == nil {
		err = os.Rename(tmpPath, path)
	}
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write upgrade marker: %w", err)
	}
	return nil
}

// removeUpgradeMarker removes the upgrade marker, which commits the state of
// the main directory.
func (u *Updater) removeUpgradeMarker() error {
	err := os.Remove(u.upgradeMarkerPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove upgrade marker: %w", err)
	}
	return nil
}

func (u *Updater) cleanupAfterUpgrade() error {
	err := os.RemoveAll(u.cfg.PurgeDirectory)
	if err != nil {
		return fmt.Errorf("delete purge dir: %w", err)
	}

	err = os.RemoveAll(u.cfg.DownloadDirectory)
	if err != nil {
		return fmt.Errorf("delete download dir: %w", err)
	}

	return nil
}

func (u *Updater) deleteUnfinishedFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		switch {
		case e.IsDir():
			// Continue.

		case strings.HasSuffix(e.Name(), ".download"):
			path := filepath.Join(dir, e.Name())
			log.Warningf("updates/%s: deleting unfinished download file: %s", u.cfg.Name, path)
			err := os.Remove(path)
			if err != nil {
				log.Errorf("updates/%s: failed to delete unfinished download file %s: %s", u.cfg.Name, path, err)
			}

		case strings.HasSuffix(e.Name(), ".copy"):
			path := filepath.Join(dir, e.Name())
			log.Warningf("updates/%s: deleting unfinished copied file: %s", u.cfg.Name, path)
			err := os.Remove(path)
			if err != nil {
				log.Errorf("updates/%s: failed to delete unfinished copied file %s: %s", u.cfg.Name, path, err)
			}
		}
	}

	return nil
}

func (u *Updater) applyPostUpgradeCommands() error {
	// At this point, we assume that the upgrade was successful and all files are in place.
	// We need to execute the post-upgrade commands, if any.

	if len(u.cfg.PostUpgradeCommands) == 0 {
		return nil
	}

	// collect full paths to files that were upgraded, required to check the trigger.
	upgradedFiles := make(map[string]struct{})
	for _, artifact := range u.index.Artifacts {
		upgradedFiles[filepath.Join(u.cfg.Directory, artifact.Filename)] = struct{}{}
	}

	// Execute post-upgrade commands.
	for _, puCmd := range u.cfg.PostUpgradeCommands {

		// Check trigger to ensure that we need to run this command.
		if len(puCmd.TriggerArtifactFName) > 0 {
			if _, ok := upgradedFiles[puCmd.TriggerArtifactFName]; !ok {
				continue
			}
		}

		log.Debugf("updates/%s: executing post-upgrade command: '%s %s'", u.cfg.Name, puCmd.Command, strings.Join(puCmd.Args, " "))
		output, err := exec.Command(puCmd.Command, puCmd.Args...).CombinedOutput()
		if err != nil {
			if puCmd.FailOnError {
				return fmt.Errorf("post-upgrade command '%s %s' failed: %w, output: %s", puCmd.Command, strings.Join(puCmd.Args, " "), err, string(output))
			}

			log.Warningf("updates/%s: post-upgrade command '%s %s' failed, but ignored. Error: %s", u.cfg.Name, puCmd.Command, strings.Join(puCmd.Args, " "), err)
		}
	}

	return nil
}
