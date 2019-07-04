package updates

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/safing/portbase/log"
)

func updater() {
	time.Sleep(10 * time.Second)
	for {
		err := CheckForUpdates()
		if err != nil {
			log.Warningf("updates: failed to check for updates: %s", err)
		}
		time.Sleep(1 * time.Hour)
	}
}

func markFileForDownload(identifier string) {
	// get file
	_, ok := localUpdates[identifier]
	// only mark if it does not yet exist
	if !ok {
		localUpdates[identifier] = "loading..."
	}
}

func markPlatformFileForDownload(identifier string) {
	// add platform prefix
	identifier = path.Join(fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH), identifier)
	// mark file
	markFileForDownload(identifier)
}

// CheckForUpdates checks if updates are available and downloads updates of used components.
func CheckForUpdates() (err error) {

	// download new index
	var data []byte
	for tries := 0; tries < 3; tries++ {
		data, err = fetchData("stable.json", tries)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}

	newStableUpdates := make(map[string]string)
	err = json.Unmarshal(data, &newStableUpdates)
	if err != nil {
		return err
	}

	if len(newStableUpdates) == 0 {
		return errors.New("stable.json is empty")
	}

	// FIXME IN STABLE: correct log line
	log.Infof("updates: downloaded new update index: stable.json (alpha until we actually reach stable)")

	// ensure important components are always updated
	updatesLock.Lock()
	if runtime.GOOS == "windows" {
		markPlatformFileForDownload("control/portmaster-control.exe")
		markPlatformFileForDownload("app/portmaster-app.exe")
		markPlatformFileForDownload("notifier/portmaster-notifier.exe")
	} else {
		markPlatformFileForDownload("control/portmaster-control")
		markPlatformFileForDownload("app/portmaster-app")
		markPlatformFileForDownload("notifier/portmaster-notifier")
	}
	updatesLock.Unlock()

	// update existing files
	log.Tracef("updates: updating existing files")
	updatesLock.RLock()
	for identifier, newVersion := range newStableUpdates {
		oldVersion, ok := localUpdates[identifier]
		if ok && newVersion != oldVersion {

			log.Tracef("updates: updating %s to %s", identifier, newVersion)
			filePath := GetVersionedPath(identifier, newVersion)
			realFilePath := filepath.Join(updateStoragePath, filePath)
			for tries := 0; tries < 3; tries++ {
				err = fetchFile(realFilePath, filePath, tries)
				if err == nil {
					break
				}
			}
			if err != nil {
				log.Warningf("updates: failed to update %s to %s: %s", identifier, newVersion, err)
			}

		}
	}
	updatesLock.RUnlock()
	log.Tracef("updates: finished updating existing files")

	// update stable index
	updatesLock.Lock()
	stableUpdates = newStableUpdates
	updatesLock.Unlock()

	// save stable index
	err = ioutil.WriteFile(filepath.Join(updateStoragePath, "stable.json"), data, 0644)
	if err != nil {
		log.Warningf("updates: failed to save new version of stable.json: %s", err)
	}

	// update version status
	updatesLock.RLock()
	defer updatesLock.RUnlock()
	updateStatus(versionClassStable, stableUpdates)

	return nil
}
