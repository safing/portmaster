package updates

import (
	"errors"
	"os"
	"runtime"

	"github.com/safing/portmaster/core/structure"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/utils"
)

const (
	isWindows = runtime.GOOS == "windows"
)

var (
	updateStorage *utils.DirStructure
	tmpStorage    *utils.DirStructure
)

// SetDataRoot sets the data root from which the updates module derives its paths.
func SetDataRoot(root *utils.DirStructure) {
	if root != nil && updateStorage == nil {
		updateStorage = root.ChildDir("updates", 0755)
		tmpStorage = updateStorage.ChildDir("tmp", 0777)
	}
}

func init() {
	modules.Register("updates", prep, start, stop, "core")
}

func prep() error {
	SetDataRoot(structure.Root())
	if updateStorage == nil {
		return errors.New("update storage path is not set")
	}

	err := updateStorage.Ensure()
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
			// download indexes
			log.Infof("updates: downloading update index...")

			err = UpdateIndexes()
			if err != nil {
				log.Errorf("updates: failed to download update index: %s", err)
			}
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
	return os.RemoveAll(tmpStorage.Path)
}
