package updates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/safing/portmaster/base/log"
)

const (
	defaultFileMode      = os.FileMode(0o0644)
	executableFileMode   = os.FileMode(0o0744)
	executableUIFileMode = os.FileMode(0o0755)
	defaultDirMode       = os.FileMode(0o0755)
)

func (u *Updater) upgrade(downloader *Downloader, ignoreVersion bool) error {
	// Lock index for the upgrade.
	u.indexLock.Lock()
	defer u.indexLock.Unlock()

	// Check if we should upgrade at all.
	if !ignoreVersion {
		if err := u.index.ShouldUpgradeTo(downloader.index); err != nil {
			return fmt.Errorf("cannot upgrade: %w", ErrNoUpdateAvailable)
		}
	}

	// Execute the upgrade.
	upgradeError := u.upgradeMoveFiles(downloader, ignoreVersion)
	if upgradeError == nil {
		return nil
	}

	// Attempt to recover from failed upgrade.
	recoveryErr := u.recoverFromFailedUpgrade()
	if recoveryErr == nil {
		return fmt.Errorf("upgrade failed, but recovery was successful: %w", upgradeError)
	}

	// Recovery failed too.
	return fmt.Errorf("upgrade (including recovery) failed: %s", u.cfg.Name, upgradeError)
}

func (u *Updater) upgradeMoveFiles(downloader *Downloader, ignoreVersion bool) error {
	// Important:
	// We assume that the downloader has done its job and all artifacts are verified.
	// Files will just be moved here.
	// In case the files are copied, they are verified in the process.

	// Reset purge directory, so that we can do a clean rollback later.
	_ = os.RemoveAll(u.cfg.PurgeDirectory)
	err := os.MkdirAll(u.cfg.PurgeDirectory, defaultDirMode)
	if err != nil {
		return fmt.Errorf("failed to create purge directory: %w", err)
	}

	// Move current version files into purge folder.
	if u.index != nil {
		log.Debugf("updates/%s: removing the old version (v%s from %s)", u.cfg.Name, u.index.Version, u.index.Published)
	}
	files, err := os.ReadDir(u.cfg.Directory)
	if err != nil {
		return fmt.Errorf("read current directory: %w", err)
	}
	for _, file := range files {
		// Check if file is ignored.
		if slices.Contains(u.cfg.Ignore, file.Name()) {
			continue
		}

		// Otherwise, move file to purge dir.
		src := filepath.Join(u.cfg.Directory, file.Name())
		dst := filepath.Join(u.cfg.PurgeDirectory, file.Name())
		err := u.moveFile(src, dst, "", file.Type().Perm())
		if err != nil {
			return fmt.Errorf("failed to move current file %s to purge dir: %w", file.Name(), err)
		}
	}

	// Move the new index file into main directory.
	log.Debugf("updates/%s: installing the new version (v%s from %s)", u.cfg.Name, downloader.index.Version, downloader.index.Published)
	src := filepath.Join(u.cfg.DownloadDirectory, u.cfg.IndexFile)
	dst := filepath.Join(u.cfg.Directory, u.cfg.IndexFile)
	err = u.moveFile(src, dst, "", defaultFileMode)
	if err != nil {
		return fmt.Errorf("failed to move index file to %s: %w", dst, err)
	}

	// Move downloaded files to the current version folder.
	for _, artifact := range downloader.index.Artifacts {
		src = filepath.Join(u.cfg.DownloadDirectory, artifact.Filename)
		dst = filepath.Join(u.cfg.Directory, artifact.Filename)
		err = u.moveFile(src, dst, artifact.SHA256, artifact.GetFileMode())
		if err != nil {
			return fmt.Errorf("failed to move file %s: %w", artifact.Filename, err)
		} else {
			log.Debugf("updates/%s: %s moved", u.cfg.Name, artifact.Filename)
		}
	}

	// Set new index on module.
	u.index = downloader.index
	log.Infof("updates/%s: update complete (v%s from %s)", u.cfg.Name, u.index.Version, u.index.Published)

	return nil
}

// moveFile moves a file and falls back to copying if it fails.
func (u *Updater) moveFile(currentPath, newPath string, sha256sum string, fileMode fs.FileMode) error {
	// Try to simply move file.
	err := os.Rename(currentPath, newPath)
	if err == nil {
		// Moving was successful, return.
		return nil
	}
	log.Tracef("updates/%s: failed to move to %q, falling back to copy+delete: %w", u.cfg.Name, newPath, err)

	// Copy and check the checksum while we are at it.
	err = copyAndCheckSHA256Sum(currentPath, newPath, sha256sum, fileMode)
	if err != nil {
		return fmt.Errorf("move failed, copy+delete fallback failed: %w", err)
	}

	return nil
}

// recoverFromFailedUpgrade attempts to roll back any moved files by the upgrade process.
func (u *Updater) recoverFromFailedUpgrade() error {
	// Get list of files from purge dir.
	files, err := os.ReadDir(u.cfg.PurgeDirectory)
	if err != nil {
		return err
	}

	// Move all files back to main dir.
	for _, file := range files {
		purgedFile := filepath.Join(u.cfg.PurgeDirectory, file.Name())
		activeFile := filepath.Join(u.cfg.Directory, file.Name())
		err := u.moveFile(purgedFile, activeFile, "", file.Type().Perm())
		if err != nil {
			// Only warn and continue to recover as many files as possible.
			log.Warningf("updates/%s: failed to roll back file %s: %w", u.cfg.Name, file.Name(), err)
		}
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
