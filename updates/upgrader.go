package updates

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/renameio"
	"github.com/safing/portbase/utils"

	"github.com/safing/portbase/log"

	processInfo "github.com/shirou/gopsutil/process"
)

const (
	upgradedSuffix = "-upgraded"
)

func runFileUpgrades() error {
	filename := "portmaster-control"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}

	// get newest portmaster-control
	newFile, err := GetPlatformFile("control/" + filename) // identifier, use forward slash!
	if err != nil {
		return err
	}

	// update portmaster-control in data root
	rootControlPath := filepath.Join(filepath.Dir(updateStoragePath), filename)
	err = upgradeFile(rootControlPath, newFile)
	if err != nil {
		return err
	}
	log.Infof("updates: upgraded %s", rootControlPath)

	// upgrade parent process, if it's portmaster-control
	parent, err := processInfo.NewProcess(int32(os.Getppid()))
	if err != nil {
		return fmt.Errorf("could not get parent process for upgrade checks: %s", err)
	}
	parentName, err := parent.Name()
	if err != nil {
		return fmt.Errorf("could not get parent process name for upgrade checks: %s", err)
	}
	if !strings.HasPrefix(parentName, filename) {
		log.Tracef("updates: parent process does not seem to be portmaster-control, name is %s", parentName)
		return nil
	}
	parentPath, err := parent.Exe()
	if err != nil {
		return fmt.Errorf("could not get parent process path for upgrade: %s", err)
	}
	err = upgradeFile(parentPath, newFile)
	if err != nil {
		return err
	}
	log.Infof("updates: upgraded %s", parentPath)

	return nil
}

func upgradeFile(fileToUpgrade string, file *File) error {
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
			// maybe we're on windows and it's in use, try moving
			// create dir
			err = utils.EnsureDirectory(downloadTmpPath, 0700)
			if err != nil {
				return fmt.Errorf("unable to create directory for upgrade process: %s", err)
			}
			// move
			err = os.Rename(fileToUpgrade, filepath.Join(
				downloadTmpPath,
				fmt.Sprintf(
					"%s-%d%s",
					GetVersionedPath(filepath.Base(fileToUpgrade), currentVersion),
					time.Now().UTC().Unix(),
					upgradedSuffix,
				),
			))
			if err != nil {
				return fmt.Errorf("unable to move file that needs upgrade: %s", err)
			}
		}
	}

	// copy upgrade
	// TODO: handle copy failure
	err = copyFile(file.Path(), fileToUpgrade)
	if err != nil {
		time.Sleep(1 * time.Second)
		// try again
		err = copyFile(file.Path(), fileToUpgrade)
		if err != nil {
			return err
		}
	}

	// check permissions
	if runtime.GOOS != "windows" {
		info, err := os.Stat(fileToUpgrade)
		if err != nil {
			return fmt.Errorf("failed to get file info on %s: %s", fileToUpgrade, err)
		}
		if info.Mode() != 0755 {
			err := os.Chmod(fileToUpgrade, 0755)
			if err != nil {
				return fmt.Errorf("failed to set permissions on %s: %s", fileToUpgrade, err)
			}
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string) (err error) {
	// open file for writing
	atomicDstFile, err := renameio.TempFile(downloadTmpPath, dstPath)
	if err != nil {
		return fmt.Errorf("could not create temp file for atomic copy: %s", err)
	}
	defer atomicDstFile.Cleanup()

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
		return fmt.Errorf("updates: failed to finalize copy to file %s: %s", dstPath, err)
	}

	return nil
}

func cleanOldUpgradedFiles() error {
	return os.RemoveAll(downloadTmpPath)
}
