package updates

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/utils"
)

var (
	updateStoragePath string
	downloadTmpPath   string
)

// SetDatabaseRoot tells the updates module where the database is - and where to put its stuff.
func SetDatabaseRoot(path string) {
	if updateStoragePath == "" {
		updateStoragePath = filepath.Join(path, "updates")
		downloadTmpPath = filepath.Join(updateStoragePath, "tmp")
	}
}

func init() {
	modules.Register("updates", prep, start, stop, "core")
}

func prep() error {
	dbRoot := database.GetDatabaseRoot()
	if dbRoot == "" {
		return errors.New("database root is not set")
	}
	updateStoragePath = filepath.Join(dbRoot, "updates")
	downloadTmpPath = filepath.Join(updateStoragePath, "tmp")

	err := utils.EnsureDirectory(updateStoragePath, 0755)
	if err != nil {
		return err
	}

	err = utils.EnsureDirectory(downloadTmpPath, 0700)
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
	// delete download tmp dir
	return os.RemoveAll(downloadTmpPath)
}
