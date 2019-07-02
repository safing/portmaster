package updates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
)

var (
	updateStoragePath string
)

// SetDatabaseRoot tells the updates module where the database is - and where to put its stuff.
func SetDatabaseRoot(path string) {
	if updateStoragePath == "" {
		updateStoragePath = filepath.Join(path, "updates")
	}
}

func init() {
	modules.Register("updates", prep, start, nil, "core")
}

func prep() error {
	dbRoot := database.GetDatabaseRoot()
	if dbRoot == "" {
		return errors.New("database root is not set")
	}
	updateStoragePath = filepath.Join(dbRoot, "updates")

	err := CheckDir(updateStoragePath)
	if err != nil {
		return err
	}

	status.Core = info.GetInfo()

	return nil
}

func start() error {
	err := initUpdateStatusHook()
	if err != nil {
		return err
	}

	err = LoadIndexes()
	if err != nil {
		if os.IsNotExist(err) {
			log.Infof("updates: stable.json does not yet exist, waiting for first update cycle")
		} else {
			return err
		}
	}

	err = LoadLatest()
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

func CheckDir(dirPath string) error {
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
