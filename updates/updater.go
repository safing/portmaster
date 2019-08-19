package updates

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/safing/portbase/log"
)

func updater() {
	time.Sleep(10 * time.Second)
	for {
		err := UpdateIndexes()
		if err != nil {
			log.Warningf("updates: updating index failed: %s", err)
		}
		err = DownloadUpdates()
		if err != nil {
			log.Warningf("updates: downloading updates failed: %s", err)
		}
		err = runFileUpgrades()
		if err != nil {
			log.Warningf("updates: failed to upgrade portmaster-control: %s", err)
		}
		err = cleanOldUpgradedFiles()
		if err != nil {
			log.Warningf("updates: failed to clean old upgraded files: %s", err)
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

// UpdateIndexes downloads the current update indexes.
func UpdateIndexes() (err error) {
	// download new indexes
	var data []byte
	for tries := 0; tries < 3; tries++ {
		data, err = fetchData("stable.json", tries)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("failed to download: %s", err)
	}

	newStableUpdates := make(map[string]string)
	err = json.Unmarshal(data, &newStableUpdates)
	if err != nil {
		return fmt.Errorf("failed to parse: %s", err)
	}

	if len(newStableUpdates) == 0 {
		return errors.New("index is empty")
	}

	// update stable index
	updatesLock.Lock()
	stableUpdates = newStableUpdates
	updatesLock.Unlock()

	// check dir
	err = updateStorage.Ensure()
	if err != nil {
		return err
	}

	// save stable index
	err = ioutil.WriteFile(filepath.Join(updateStorage.Path, "stable.json"), data, 0644)
	if err != nil {
		log.Warningf("updates: failed to save new version of stable.json: %s", err)
	}

	// update version status
	updatesLock.RLock()
	updateStatus(versionClassStable, stableUpdates)
	updatesLock.RUnlock()

	// FIXME IN STABLE: correct log line
	log.Infof("updates: updated index stable.json (alpha/beta until we actually reach stable)")

	return nil
}

// DownloadUpdates checks if updates are available and downloads updates of used components.
func DownloadUpdates() (err error) {

	// ensure important components are always updated
	updatesLock.Lock()
	if runtime.GOOS == "windows" {
		markPlatformFileForDownload("core/portmaster-core.exe")
		markPlatformFileForDownload("control/portmaster-control.exe")
		markPlatformFileForDownload("app/portmaster-app.exe")
		markPlatformFileForDownload("notifier/portmaster-notifier.exe")
		markPlatformFileForDownload("notifier/portmaster-snoretoast.exe")
	} else {
		markPlatformFileForDownload("core/portmaster-core")
		markPlatformFileForDownload("control/portmaster-control")
		markPlatformFileForDownload("app/portmaster-app")
		markPlatformFileForDownload("notifier/portmaster-notifier")
	}
	updatesLock.Unlock()

	// check download dir
	err = tmpStorage.Ensure()
	if err != nil {
		return fmt.Errorf("could not prepare tmp directory for download: %s", err)
	}

	// RLock for the remaining function
	updatesLock.RLock()
	defer updatesLock.RUnlock()

	// update existing files
	log.Tracef("updates: updating existing files")
	for identifier, newVersion := range stableUpdates {
		oldVersion, ok := localUpdates[identifier]
		if ok && newVersion != oldVersion {

			log.Tracef("updates: updating %s to %s", identifier, newVersion)
			filePath := GetVersionedPath(identifier, newVersion)
			realFilePath := filepath.Join(updateStorage.Path, filePath)
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
	log.Tracef("updates: finished updating existing files")

	// remove tmp folder after we are finished
	err = os.RemoveAll(tmpStorage.Path)
	if err != nil {
		log.Tracef("updates: failed to remove tmp dir %s after downloading updates: %s", updateStorage.Path, err)
	}

	return nil
}
