package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates"
)

// TODO: reduce writes by comparing hashes before copying

const (
	onWindows = runtime.GOOS == "windows"

	webviewFileIdentifier       = "app/webview.dll"
	webviewLoaderFileIdentifier = "app/WebView2Loader.dll"
)

var (
	dllUpgraderLock sync.Mutex

	webviewFile       *updater.File
	webviewLoaderFile *updater.File
)

func initDLLCopier() error {
	if onWindows {
		return module.RegisterEventHook(
			updates.ModuleName,
			updates.ResourceUpdateEvent,
			"copy DLLs for app",
			copyDLLs,
		)
	}

	return nil
}

func copyDLLs(_ context.Context, _ interface{}) (err error) {
	dllUpgraderLock.Lock()
	defer dllUpgraderLock.Unlock()

	err = copyDLL(webviewFileIdentifier, &webviewFile)
	if err != nil {
		return err
	}

	err = copyDLL(webviewLoaderFileIdentifier, &webviewLoaderFile)
	if err != nil {
		return err
	}

	return nil
}

func copyDLL(identifier string, filePtr **updater.File) (err error) {
	file := *filePtr
	copyFile := false

	if file == nil {
		file, err = updates.GetPlatformFile(identifier)
		if err != nil {
			return fmt.Errorf("failed to get DLL %s: %w", identifier, err)
		}
		*filePtr = file // set global var
		copyFile = true
	} else {
		if file.UpgradeAvailable() {
			copyFile = true
		}
	}

	if copyFile {
		dstPath, _, ok := updater.GetIdentifierAndVersion(filepath.ToSlash(file.Path()))
		if !ok {
			return fmt.Errorf("DLL file path invalid: %s", file.Path())
		}
		dstPath = filepath.FromSlash(dstPath)

		err = updates.CopyFile(file.Path(), dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy DLL %s: %w", identifier, err)
		}

		log.Debugf("ui: copied %s to %s to make it available to the app", identifier, dstPath)
	}

	return nil
}
