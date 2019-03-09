package updates

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/info"
	"github.com/Safing/portbase/modules"

	// module dependencies
	_ "github.com/Safing/portmaster/core"
)

var (
	updateStoragePath string
)

func init() {
	modules.Register("updates", prep, start, nil, "core")
}

func prep() error {
	status.Core = info.GetInfo()
	updateStoragePath = filepath.Join(database.GetDatabaseRoot(), "updates")

	err := checkUpdateDirs()
	if err != nil {
		return err
	}

	err = upgradeByFlag()
	if err != nil {
		return err
	}

	return nil
}

func start() error {
	err := initUpdateStatusHook()
	if err != nil {
		return err
	}

	err = ReloadLatest()
	if err != nil {
		return err
	}

	go updater()
	go updateNotifier()
	return nil
}

func stop() error {
	return os.RemoveAll(filepath.Join(updateStoragePath, "tmp"))
}

func checkUpdateDirs() error {
	// all
	err := checkDir(filepath.Join(updateStoragePath, "all"))
	if err != nil {
		return err
	}

	// os_platform
	err = checkDir(filepath.Join(updateStoragePath, fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)))
	if err != nil {
		return err
	}

	// tmp
	err = checkDir(filepath.Join(updateStoragePath, "tmp"))
	if err != nil {
		return err
	}

	return nil
}

func checkDir(dirPath string) error {
	f, err := os.Stat(dirPath)
	if err == nil {
		// file exists
		if f.IsDir() {
			return nil
		}
		err = os.Remove(dirPath)
		if err != nil {
			return fmt.Errorf("could not remove file %s to place dir: %s", dirPath, err)
		}
		err = os.MkdirAll(dirPath, 0755)
		if err != nil {
			return fmt.Errorf("could not create dir %s: %s", dirPath, err)
		}
		return nil
	}
	// file does not exist
	if os.IsNotExist(err) {
		err = os.MkdirAll(dirPath, 0755)
		if err != nil {
			return fmt.Errorf("could not create dir %s: %s", dirPath, err)
		}
		return nil
	}
	// other error
	return fmt.Errorf("failed to access %s: %s", dirPath, err)
}
