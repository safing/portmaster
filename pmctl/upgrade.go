package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safing/portbase/info"
	"github.com/safing/portmaster/updates"
)

func checkForUpgrade() (update *updates.File) {
	info := info.GetInfo()
	file, err := updates.GetLocalPlatformFile("control/portmaster-control")
	if err != nil {
		return nil
	}
	if info.Version != file.Version() {
		return file
	}
	return nil
}

func doSelfUpgrade(file *updates.File) error {

	// get destination
	dst, err := os.Executable()
	if err != nil {
		return err
	}
	dst, err = filepath.EvalSymlinks(dst)
	if err != nil {
		return err
	}

	// mv destination
	err = os.Rename(dst, dst+"_old")
	if err != nil {
		return err
	}

	// hard link
	err = os.Link(file.Path(), dst)
	if err != nil {
		fmt.Printf("%s failed to hardlink self upgrade: %s, will copy...\n", logPrefix, err)
		err = copyFile(file.Path(), dst)
		if err != nil {
			return err
		}
	}

	// check permission
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			return fmt.Errorf("failed to get file info on %s: %s", dst, err)
		}
		if info.Mode() != 0755 {
			err := os.Chmod(dst, 0755)
			if err != nil {
				return fmt.Errorf("failed to set permissions on %s: %s", dst, err)
			}
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string) (err error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return
	}
	defer func() {
		closeErr := dstFile.Close()
		if err == nil {
			err = closeErr
		}
	}()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return
	}
	err = dstFile.Sync()
	return
}

func removeOldBin() error {
	// get location
	dst, err := os.Executable()
	if err != nil {
		return err
	}
	dst, err = filepath.EvalSymlinks(dst)
	if err != nil {
		return err
	}

	// delete old
	err = os.Remove(dst + "_old")
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	fmt.Println("removed previous portmaster-control")
	return nil
}
