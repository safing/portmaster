package updates

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Safing/portbase/modules"
)

const (
	coreIdentifier = "core/portmaster"
)

var (
	upgradeSelf bool
)

func init() {
	flag.BoolVar(&upgradeSelf, "upgrade", false, "upgrade to newest portmaster core binary")
}

func upgradeByFlag() error {
	if !upgradeSelf {
		return nil
	}

	err := ReloadLatest()
	if err != nil {
		return err
	}

	return doSelfUpgrade()
}

func doSelfUpgrade() error {

	// get source
	file, err := GetPlatformFile(coreIdentifier)
	if err != nil {
		return fmt.Errorf("%s currently not available: %s - you may need to first start portmaster and wait for it to fetch the update index", coreIdentifier, err)
	}

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
		fmt.Printf("failed to hardlink: %s, will copy...\n", err)
		err = copyFile(file.Path(), dst)
		if err != nil {
			return err
		}
	}

	// check permission
	info, err := os.Stat(dst)
	if info.Mode() != 0755 {
		err := os.Chmod(dst, 0755)
		if err != nil {
			return fmt.Errorf("failed to set permissions on %s: %s", dst, err)
		}
	}

	// delete old
	err = os.Remove(dst + "_old")
	if err != nil {
		return err
	}

	// gracefully exit portmaster
	return modules.ErrCleanExit
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
