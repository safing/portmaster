//go:build windows
// +build windows

package windowskext

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows"
)

func createService(manager windows.Handle, portmasterKextPath *uint16) (windows.Handle, error) {
	u16filename, err := syscall.UTF16FromString(driverName)
	if err != nil {
		return 0, fmt.Errorf("Bad service: %s", err)
	}
	// Check if it's already created
	service, err := windows.OpenService(manager, &u16filename[0], windows.SERVICE_ALL_ACCESS)
	if err == nil {
		return service, nil
	}

	// Create the service
	service, err = windows.CreateService(manager, &u16filename[0], &u16filename[0], windows.SERVICE_ALL_ACCESS, windows.SERVICE_KERNEL_DRIVER, windows.SERVICE_DEMAND_START, windows.SERVICE_ERROR_NORMAL, portmasterKextPath, nil, nil, nil, nil, nil)
	if err != nil {
		return 0, err
	}

	return service, nil
}

func driverInstall(portmasterKextPath string) (windows.Handle, error) {
	u16kextPath, _ := syscall.UTF16FromString(portmasterKextPath)
	// Open the service manager:
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS)
	if err != nil {
		return 0, fmt.Errorf("Failed to open service manager: %d", err)
	}
	defer windows.CloseServiceHandle(manager)

	// Try to create the service. Retry if it fails.
	var service windows.Handle
retryLoop:
	for i := 0; i < 3; i++ {
		service, err = createService(manager, &u16kextPath[0])
		if err == nil {
			break retryLoop
		}
	}

	if err != nil {
		return 0, fmt.Errorf("Failed to create service: %s", err)
	}

	// Start the service:
	err = windows.StartService(service, 0, nil)

	if err != nil {
		err = windows.GetLastError()
		if err != windows.ERROR_SERVICE_ALREADY_RUNNING {
			// Failed to start service; clean-up:
			var status windows.SERVICE_STATUS
			_ = windows.ControlService(service, windows.SERVICE_CONTROL_STOP, &status)
			_ = windows.DeleteService(service)
			_ = windows.CloseServiceHandle(service)
			service = 0
		}
	}

	return service, nil
}

func openDriver(filename string) (windows.Handle, error) {
	u16filename, _ := syscall.UTF16FromString(filename)

	handle, err := windows.CreateFile(&u16filename[0], windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return 0, err
	}

	return handle, nil
}

func closeDriver(handle windows.Handle) error {
	return windows.CloseHandle(handle)
}
