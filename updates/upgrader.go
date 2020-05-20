package updates

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tevino/abool"

	"github.com/google/renameio"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portbase/updater"

	processInfo "github.com/shirou/gopsutil/process"
)

const (
	upgradedSuffix = "-upgraded"
)

var (
	upgraderActive = abool.NewBool(false)
	pmCtrlUpdate   *updater.File
	pmCoreUpdate   *updater.File

	rawVersionRegex = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+b?\*?$`)
)

func initUpgrader() error {
	return module.RegisterEventHook(
		ModuleName,
		ResourceUpdateEvent,
		"run upgrades",
		upgrader,
	)
}

func upgrader(_ context.Context, _ interface{}) error {
	// like a lock, but discard additional runs
	if !upgraderActive.SetToIf(false, true) {
		return nil
	}
	defer upgraderActive.SetTo(false)

	// upgrade portmaster control
	err := upgradePortmasterControl()
	if err != nil {
		log.Warningf("updates: failed to upgrade portmaster-control: %s", err)
	}

	err = upgradeCoreNotify()
	if err != nil {
		log.Warningf("updates: failed to notify about core upgrade: %s", err)
	}

	return nil
}

func upgradeCoreNotify() error {
	identifier := "core/portmaster-core" // identifier, use forward slash!
	if onWindows {
		identifier += ".exe"
	}

	// check if we can upgrade
	if pmCoreUpdate == nil || pmCoreUpdate.UpgradeAvailable() {
		// get newest portmaster-core
		new, err := GetPlatformFile(identifier)
		if err != nil {
			return err
		}
		pmCoreUpdate = new
	} else {
		return nil
	}

	if info.GetInfo().Version != pmCoreUpdate.Version() {
		notifications.NotifyInfo(
			"updates-core-update-available",
			fmt.Sprintf("There is an update available for the Portmaster core (v%s), please restart the Portmaster to apply the update.", pmCoreUpdate.Version()),
		)

		log.Debugf("updates: new portmaster version available, sending notification to user")
	}

	return nil
}

func upgradePortmasterControl() error {
	filename := "portmaster-control"
	if onWindows {
		filename += ".exe"
	}

	// check if we can upgrade
	if pmCtrlUpdate == nil || pmCtrlUpdate.UpgradeAvailable() {
		// get newest portmaster-control
		new, err := GetPlatformFile("control/" + filename) // identifier, use forward slash!
		if err != nil {
			return err
		}
		pmCtrlUpdate = new
	} else {
		return nil
	}

	// update portmaster-control in data root
	rootControlPath := filepath.Join(filepath.Dir(registry.StorageDir().Path), filename)
	err := upgradeFile(rootControlPath, pmCtrlUpdate)
	if err != nil {
		return err
	}
	log.Infof("updates: upgraded %s", rootControlPath)

	// upgrade parent process, if it's portmaster-control
	parent, err := processInfo.NewProcess(int32(os.Getppid()))
	if err != nil {
		return fmt.Errorf("could not get parent process for upgrade checks: %w", err)
	}
	parentName, err := parent.Name()
	if err != nil {
		return fmt.Errorf("could not get parent process name for upgrade checks: %w", err)
	}
	if parentName != filename {
		log.Tracef("updates: parent process does not seem to be portmaster-control, name is %s", parentName)
		return nil
	}
	parentPath, err := parent.Exe()
	if err != nil {
		return fmt.Errorf("could not get parent process path for upgrade: %w", err)
	}
	err = upgradeFile(parentPath, pmCtrlUpdate)
	if err != nil {
		return err
	}
	log.Infof("updates: upgraded %s", parentPath)

	return nil
}

func upgradeFile(fileToUpgrade string, file *updater.File) error {
	fileExists := false
	_, err := os.Stat(fileToUpgrade)
	if err == nil {
		// file exists and is accessible
		fileExists = true
	}

	if fileExists {
		// get current version
		var currentVersion string
		cmd := exec.Command(fileToUpgrade, "--ver")
		out, err := cmd.Output()
		if err == nil {
			// abort if version matches
			currentVersion = strings.Trim(strings.TrimSpace(string(out)), "*")
			if currentVersion == file.Version() {
				// already up to date!
				return nil
			}
		} else {
			log.Warningf("updates: failed to run %s to get version for upgrade check: %s", fileToUpgrade, err)
			currentVersion = "0.0.0"
		}

		// test currentVersion for sanity
		if !rawVersionRegex.MatchString(currentVersion) {
			log.Tracef("updates: version string returned by %s is invalid: %s", fileToUpgrade, currentVersion)
			currentVersion = "0.0.0"
		}

		// try removing old version
		err = os.Remove(fileToUpgrade)
		if err != nil {
			// ensure tmp dir is here
			err = registry.TmpDir().Ensure()
			if err != nil {
				return fmt.Errorf("could not prepare tmp directory for moving file that needs upgrade: %w", err)
			}

			// maybe we're on windows and it's in use, try moving
			err = os.Rename(fileToUpgrade, filepath.Join(
				registry.TmpDir().Path,
				fmt.Sprintf(
					"%s-%d%s",
					updater.GetVersionedPath(filepath.Base(fileToUpgrade), currentVersion),
					time.Now().UTC().Unix(),
					upgradedSuffix,
				),
			))
			if err != nil {
				return fmt.Errorf("unable to move file that needs upgrade: %w", err)
			}
		}
	}

	// copy upgrade
	err = CopyFile(file.Path(), fileToUpgrade)
	if err != nil {
		// try again
		time.Sleep(1 * time.Second)
		err = CopyFile(file.Path(), fileToUpgrade)
		if err != nil {
			return err
		}
	}

	// check permissions
	if !onWindows {
		info, err := os.Stat(fileToUpgrade)
		if err != nil {
			return fmt.Errorf("failed to get file info on %s: %w", fileToUpgrade, err)
		}
		if info.Mode() != 0755 {
			err := os.Chmod(fileToUpgrade, 0755)
			if err != nil {
				return fmt.Errorf("failed to set permissions on %s: %w", fileToUpgrade, err)
			}
		}
	}
	return nil
}

// CopyFile atomically copies a file using the update registry's tmp dir.
func CopyFile(srcPath, dstPath string) (err error) {

	// check tmp dir
	err = registry.TmpDir().Ensure()
	if err != nil {
		return fmt.Errorf("could not prepare tmp directory for copying file: %w", err)
	}

	// open file for writing
	atomicDstFile, err := renameio.TempFile(registry.TmpDir().Path, dstPath)
	if err != nil {
		return fmt.Errorf("could not create temp file for atomic copy: %w", err)
	}
	defer atomicDstFile.Cleanup() //nolint:errcheck // ignore error for now, tmp dir will be cleaned later again anyway

	// open source
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return
	}
	defer srcFile.Close()

	// copy data
	_, err = io.Copy(atomicDstFile, srcFile)
	if err != nil {
		return
	}

	// finalize file
	err = atomicDstFile.CloseAtomicallyReplace()
	if err != nil {
		return fmt.Errorf("updates: failed to finalize copy to file %s: %w", dstPath, err)
	}

	return nil
}
