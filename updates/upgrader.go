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
	exeExt         = ".exe"
)

var (
	// UpgradeCore specifies if portmaster-core should be upgraded.
	UpgradeCore = true

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

	// upgrade portmaster-start
	err := upgradePortmasterStart()
	if err != nil {
		log.Warningf("updates: failed to upgrade portmaster-start: %s", err)
	}

	if UpgradeCore {
		err = upgradeCoreNotify()
		if err != nil {
			log.Warningf("updates: failed to notify about core upgrade: %s", err)
		}
	}

	return nil
}

func upgradeCoreNotify() error {
	identifier := "core/portmaster-core" // identifier, use forward slash!
	if onWindows {
		identifier += exeExt
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
		n := notifications.NotifyInfo(
			"updates:core-update-available",
			fmt.Sprintf("There is an update available for the Portmaster core (v%s), please restart the Portmaster to apply the update.", pmCoreUpdate.Version()),
			notifications.Action{
				ID:   "later",
				Text: "Later",
			},
			notifications.Action{
				ID:   "restart",
				Text: "Restart Portmaster Now",
			},
		)
		n.SetActionFunction(upgradeCoreNotifyActionHandler)

		log.Debugf("updates: new portmaster version available, sending notification to user")
	}

	return nil
}

func upgradeCoreNotifyActionHandler(n *notifications.Notification) {
	switch n.SelectedActionID {
	case "restart":
		// Cannot directly trigger due to import loop.
		err := module.InjectEvent(
			"user triggered restart via notification",
			"core",
			"restart",
			nil,
		)
		if err != nil {
			log.Warningf("updates: failed to trigger restart via notification: %s", err)
		}
	case "later":
		n.Expires = time.Now().Unix() // expire immediately
	}
}

func upgradePortmasterStart() error {
	filename := "portmaster-start"
	if onWindows {
		filename += exeExt
	}

	// check if we can upgrade
	if pmCtrlUpdate == nil || pmCtrlUpdate.UpgradeAvailable() {
		// get newest portmaster-start
		new, err := GetPlatformFile("start/" + filename) // identifier, use forward slash!
		if err != nil {
			return err
		}
		pmCtrlUpdate = new
	} else {
		return nil
	}

	// update portmaster-start in data root
	rootPmStartPath := filepath.Join(filepath.Dir(registry.StorageDir().Path), filename)
	err := upgradeFile(rootPmStartPath, pmCtrlUpdate)
	if err != nil {
		return err
	}
	log.Infof("updates: upgraded %s", rootPmStartPath)

	return nil
}

func warnOnIncorrectParentPath() {
	expectedFileName := "portmaster-start"
	if onWindows {
		expectedFileName += exeExt
	}

	// upgrade parent process, if it's portmaster-start
	parent, err := processInfo.NewProcess(int32(os.Getppid()))
	if err != nil {
		log.Tracef("could not get parent process: %s", err)
		return
	}
	parentName, err := parent.Name()
	if err != nil {
		log.Tracef("could not get parent process name: %s", err)
		return
	}
	if parentName != expectedFileName {
		log.Warningf("updates: parent process does not seem to be portmaster-start, name is %s", parentName)

		// TODO(ppacher): once we released a new installer and folks had time
		//                to update we should send a module warning/hint to the
		//                UI notifying the user that he's still using portmaster-control.
		return
	}

	parentPath, err := parent.Exe()
	if err != nil {
		log.Tracef("could not get parent process path: %s", err)
		return
	}

	absPath, err := filepath.Abs(parentPath)
	if err != nil {
		log.Tracef("could not get absolut parent process path: %s", err)
		return
	}

	root := filepath.Dir(registry.StorageDir().Path)
	if !strings.HasPrefix(absPath, root) {
		log.Warningf("detected unexpected path %s for portmaster-start", absPath)

		notifications.NotifyWarn(
			"updates:unsupported-parent",
			fmt.Sprintf("The portmaster has been launched by an unexpected %s binary at %s. Please configure your system to use the binary at %s as this version will be kept up to date automatically.", expectedFileName, absPath, filepath.Join(root, expectedFileName)),
		)
	}
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
		cmd := exec.Command(fileToUpgrade, "version", "--short")
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
					filepath.Base(fileToUpgrade),
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
