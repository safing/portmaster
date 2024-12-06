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

	processInfo "github.com/shirou/gopsutil/process"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/base/utils/renameio"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates/helper"
)

const (
	upgradedSuffix = "-upgraded"
	exeExt         = ".exe"
)

var (
	upgraderActive = abool.NewBool(false)

	pmCtrlUpdate *updater.File
	pmCoreUpdate *updater.File

	spnHubUpdate *updater.File

	rawVersionRegex = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+b?\*?$`)
)

func initUpgrader() error {
	module.EventResourcesUpdated.AddCallback("run upgrades", upgrader)
	return nil
}

func upgrader(m *mgr.WorkerCtx, _ struct{}) (cancel bool, err error) {
	// Lock runs, but discard additional runs.
	if !upgraderActive.SetToIf(false, true) {
		return false, nil
	}
	defer upgraderActive.SetTo(false)

	// Upgrade portmaster-start.
	err = upgradePortmasterStart()
	if err != nil {
		log.Warningf("updates: failed to upgrade portmaster-start: %s", err)
	}

	// Upgrade based on binary.
	binBaseName := strings.Split(filepath.Base(os.Args[0]), "_")[0]
	switch binBaseName {
	case "portmaster-core":
		// Notify about upgrade.
		if err := upgradeCoreNotify(); err != nil {
			log.Warningf("updates: failed to notify about core upgrade: %s", err)
		}

		// Fix chrome sandbox permissions.
		if err := helper.EnsureChromeSandboxPermissions(registry); err != nil {
			log.Warningf("updates: failed to handle electron upgrade: %s", err)
		}

		// Upgrade system integration.
		upgradeSystemIntegration()

	case "spn-hub":
		// Trigger upgrade procedure.
		if err := upgradeHub(); err != nil {
			log.Warningf("updates: failed to initiate hub upgrade: %s", err)
		}
	}

	return false, nil
}

func upgradeCoreNotify() error {
	if pmCoreUpdate != nil && !pmCoreUpdate.UpgradeAvailable() {
		return nil
	}

	// make identifier
	identifier := "core/portmaster-core" // identifier, use forward slash!
	if onWindows {
		identifier += exeExt
	}

	// get newest portmaster-core
	newFile, err := GetPlatformFile(identifier)
	if err != nil {
		return err
	}
	pmCoreUpdate = newFile

	// check for new version
	if info.VersionNumber() != pmCoreUpdate.Version() {
		n := notifications.Notify(&notifications.Notification{
			EventID: "updates:core-update-available",
			Type:    notifications.Info,
			Title: fmt.Sprintf(
				"Portmaster Update v%s Is Ready!",
				pmCoreUpdate.Version(),
			),
			Category: "Core",
			Message: fmt.Sprintf(
				`A new Portmaster version is ready to go! Restart the Portmaster to upgrade to %s.`,
				pmCoreUpdate.Version(),
			),
			ShowOnSystem: true,
			AvailableActions: []*notifications.Action{
				// TODO: Use special UI action in order to reload UI on restart.
				{
					ID:   "restart",
					Text: "Restart",
				},
				{
					ID:   "later",
					Text: "Not now",
				},
			},
		})
		n.SetActionFunction(upgradeCoreNotifyActionHandler)

		log.Debugf("updates: new portmaster version available, sending notification to user")
	}

	return nil
}

func upgradeCoreNotifyActionHandler(_ context.Context, n *notifications.Notification) error {
	switch n.SelectedActionID {
	case "restart":
		log.Infof("updates: user triggered restart via core update notification")
		RestartNow()
	case "later":
		n.Delete()
	}

	return nil
}

func upgradeHub() error {
	if spnHubUpdate != nil && !spnHubUpdate.UpgradeAvailable() {
		return nil
	}

	// Make identifier for getting file from updater.
	identifier := "hub/spn-hub" // identifier, use forward slash!
	if onWindows {
		identifier += exeExt
	}

	// Get newest spn-hub file.
	newFile, err := GetPlatformFile(identifier)
	if err != nil {
		return err
	}
	spnHubUpdate = newFile

	// Check if the new version is different.
	if info.GetInfo().Version != spnHubUpdate.Version() {
		// Get random delay with up to three hours.
		delayMinutes, err := rng.Number(3 * 60)
		if err != nil {
			return err
		}

		// Delay restart for at least one hour for preparations.
		DelayedRestart(time.Duration(delayMinutes+60) * time.Minute)

		// Increase update checks in order to detect aborts better.
		if !disableTaskSchedule {
			module.updateWorkerMgr.Repeat(10 * time.Minute)
		}
	} else {
		AbortRestart()

		// Set update task schedule back to normal.
		if !disableTaskSchedule {
			module.updateWorkerMgr.Repeat(updateTaskRepeatDuration)
		}
	}

	return nil
}

func upgradePortmasterStart() error {
	filename := "portmaster-start"
	if onWindows {
		filename += exeExt
	}

	// check if we can upgrade
	if pmCtrlUpdate == nil || pmCtrlUpdate.UpgradeAvailable() {
		// get newest portmaster-start
		newFile, err := GetPlatformFile("start/" + filename) // identifier, use forward slash!
		if err != nil {
			return err
		}
		pmCtrlUpdate = newFile
	} else {
		return nil
	}

	// update portmaster-start in data root
	rootPmStartPath := filepath.Join(dataroot.Root().Path, filename)
	err := upgradeBinary(rootPmStartPath, pmCtrlUpdate)
	if err != nil {
		return err
	}

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
		// Only warn about this if not in dev mode.
		if !devMode() {
			log.Warningf("updates: parent process does not seem to be portmaster-start, name is %s", parentName)
		}

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
			"Unsupported Launcher",
			fmt.Sprintf(
				"The Portmaster has been launched by an unexpected %s binary at %s. Please configure your system to use the binary at %s as this version will be kept up to date automatically.",
				expectedFileName,
				absPath,
				filepath.Join(root, expectedFileName),
			),
		)
	}
}

func upgradeBinary(fileToUpgrade string, file *updater.File) error {
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
				log.Debugf("updates: %s is already v%s", fileToUpgrade, file.Version())
				// already up to date!
				return nil
			}
		} else {
			log.Warningf("updates: failed to run %s to get version for upgrade check: %s", fileToUpgrade, err)
			currentVersion = "0.0.0"
		}

		// test currentVersion for sanity
		if !rawVersionRegex.MatchString(currentVersion) {
			log.Debugf("updates: version string returned by %s is invalid: %s", fileToUpgrade, currentVersion)
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
	if onWindows {
		_ = utils.SetExecPermission(fileToUpgrade, utils.PublicReadPermission)
	} else {
		perm := utils.PublicReadPermission
		info, err := os.Stat(fileToUpgrade)
		if err != nil {
			return fmt.Errorf("failed to get file info on %s: %w", fileToUpgrade, err)
		}
		if info.Mode() != perm.AsUnixDirExecPermission() {
			err = utils.SetExecPermission(fileToUpgrade, perm)
			if err != nil {
				return fmt.Errorf("failed to set permissions on %s: %w", fileToUpgrade, err)
			}
		}
	}

	log.Infof("updates: upgraded %s to v%s", fileToUpgrade, file.Version())
	return nil
}

// CopyFile atomically copies a file using the update registry's tmp dir.
func CopyFile(srcPath, dstPath string) error {
	// check tmp dir
	err := registry.TmpDir().Ensure()
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
		return err
	}
	defer func() {
		_ = srcFile.Close()
	}()

	// copy data
	_, err = io.Copy(atomicDstFile, srcFile)
	if err != nil {
		return err
	}

	// finalize file
	err = atomicDstFile.CloseAtomicallyReplace()
	if err != nil {
		return fmt.Errorf("updates: failed to finalize copy to file %s: %w", dstPath, err)
	}

	return nil
}
