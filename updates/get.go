package updates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Safing/portbase/log"
)

var (
	ErrNotFound = errors.New("the requested file could not be found")
)

// GetPlatformFile returns the latest platform specific file identified by the given identifier.
func GetPlatformFile(identifier string) (*File, error) {
	identifier = filepath.Join(fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH), identifier)
	// From https://golang.org/pkg/runtime/#GOARCH
	// GOOS is the running program's operating system target: one of darwin, freebsd, linux, and so on.
	// GOARCH is the running program's architecture target: one of 386, amd64, arm, s390x, and so on.
	return loadOrFetchFile(identifier)
}

// GetFile returns the latest generic file identified by the given identifier.
func GetFile(identifier string) (*File, error) {
	identifier = filepath.Join("all", identifier)
	return loadOrFetchFile(identifier)
}

func getLatestFilePath(identifier string) (versionedFilePath, version string, stable bool, ok bool) {
	updatesLock.RLock()
	version, ok = stableUpdates[identifier]
	if !ok {
		version, ok = localUpdates[identifier]
		if !ok {
			log.Tracef("updates: file %s does not exist", identifier)
			return "", "", false, false
			// TODO: if in development mode, reload latest index to check for newly sideloaded updates
			// err := reloadLatest()
		}
	}
	updatesLock.RUnlock()

	// TODO: Fix for stable release
	return getVersionedPath(identifier, version), version, false, true
}

func loadOrFetchFile(identifier string) (*File, error) {
	versionedFilePath, version, stable, ok := getLatestFilePath(identifier)
	if !ok {
		// TODO: if in development mode, search updates dir for sideloaded apps
		return nil, ErrNotFound
	}

	// build final filepath
	realFilePath := filepath.Join(updateStoragePath, versionedFilePath)
	if _, err := os.Stat(realFilePath); err == nil {
		// file exists
		updateUsedStatus(identifier, version)
		return newFile(realFilePath, version, stable), nil
	}

	// download file
	log.Tracef("updates: starting download of %s", versionedFilePath)
	var err error
	for tries := 0; tries < 5; tries++ {
		err := fetchFile(realFilePath, versionedFilePath, tries)
		if err != nil {
			log.Tracef("updates: failed to download %s: %s, retrying (%d)", versionedFilePath, err, tries+1)
		} else {
			updateUsedStatus(identifier, version)
			return newFile(realFilePath, version, stable), nil
		}
	}
	log.Warningf("updates: failed to download %s: %s", versionedFilePath, err)
	return nil, err
}
