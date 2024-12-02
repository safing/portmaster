//go:build windows
// +build windows

package integration

import (
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
	"golang.org/x/sys/windows"
)

type OSSpecific struct {
	dll          *windows.DLL
	etwFunctions *ETWFunctions
}

// Initialize loads the dll and finds all the needed functions from it.
func (i *OSIntegration) Initialize() error {
	// Try to load dll
	err := i.loadDLL()
	if err != nil {
		log.Errorf("integration: failed to load dll: %s", err)

		callbackLock := sync.Mutex{}
		// listen for event from the updater and try to load again if any.
		i.instance.Updates().EventResourcesUpdated.AddCallback("core-dll-loader", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
			// Make sure no multiple callas are executed at the same time.
			callbackLock.Lock()
			defer callbackLock.Unlock()

			// Try to load again.
			err = i.loadDLL()
			if err != nil {
				log.Errorf("integration: failed to load dll: %s", err)
			} else {
				log.Info("integration: initialize successful after updater event")
			}
			return false, nil
		})

	} else {
		log.Info("integration: initialize successful")
	}
	return nil
}

func (i *OSIntegration) loadDLL() error {
	// Find path to the dll.
	file, err := updates.GetPlatformFile("dll/portmaster-core.dll")
	if err != nil {
		return err
	}
	// Load the DLL.
	i.os.dll, err = windows.LoadDLL(file.Path())
	if err != nil {
		return fmt.Errorf("failed to load dll: %q", err)
	}

	// Enumerate all needed dll functions.
	i.os.etwFunctions, err = initializeETW(i.os.dll)
	if err != nil {
		return err
	}

	// Notify listeners
	i.OnInitializedEvent.Submit(struct{}{})

	return nil
}

// CleanUp releases any resources allocated during initialization.
func (i *OSIntegration) CleanUp() error {
	if i.os.dll != nil {
		return i.os.dll.Release()
	}
	return nil
}

// GetETWInterface return struct containing all the ETW related functions, and nil if it was not loaded yet
func (i *OSIntegration) GetETWInterface() *ETWFunctions {
	return i.os.etwFunctions
}
