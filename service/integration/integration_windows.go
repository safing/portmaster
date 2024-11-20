//go:build windows
// +build windows

package integration

import (
	"fmt"

	"github.com/safing/portmaster/service/updates"
	"golang.org/x/sys/windows"
)

type OSSpecific struct {
	dll          *windows.DLL
	etwFunctions ETWFunctions
}

// Initialize loads the dll and finds all the needed functions from it.
func (i *OSIntegration) Initialize() error {
	// Find path to the dll.
	file, err := updates.GetFile("portmaster-core.dll")
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

	return nil
}

// CleanUp releases any resourses allocated during initializaion.
func (i *OSIntegration) CleanUp() error {
	if i.os.dll != nil {
		return i.os.dll.Release()
	}
	return nil
}

// GetETWInterface return struct containing all the ETW related functions.
func (i *OSIntegration) GetETWInterface() ETWFunctions {
	return i.os.etwFunctions
}
