//go:build windows
// +build windows

package windowskext

import (
	"fmt"
	"syscall"
	"time"

	"github.com/safing/portmaster/base/log"
	"golang.org/x/sys/windows"
)

type KextService struct {
	handle windows.Handle
}

func createKextService(driverName string, driverPath string) (*KextService, error) {
	// Open the service manager:
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS)
	if err != nil {
		return nil, fmt.Errorf("failed to open service manager: %d", err)
	}
	defer windows.CloseServiceHandle(manager)

	// Convert the driver name to a UTF16 string
	driverNameU16, err := syscall.UTF16FromString(driverName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver name to UTF16 string: %w", err)
	}

	// Check if there is an old service.
	service, err := windows.OpenService(manager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
	if err == nil {
		log.Warning("kext: old driver service was found")
		oldService := &KextService{handle: service}
		err := deleteService(manager, oldService, driverNameU16)
		if err != nil {
			return nil, fmt.Errorf("failed to delete old driver service: %s", err)
		}

		service = winInvalidHandleValue
		log.Info("kext: old driver service was deleted successfully")
	}

	driverPathU16, err := syscall.UTF16FromString(driverPath)

	// Create the service
	service, err = windows.CreateService(manager, &driverNameU16[0], &driverNameU16[0], windows.SERVICE_ALL_ACCESS, windows.SERVICE_KERNEL_DRIVER, windows.SERVICE_DEMAND_START, windows.SERVICE_ERROR_NORMAL, &driverPathU16[0], nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	return &KextService{handle: service}, nil
}

func deleteService(manager windows.Handle, service *KextService, driverName []uint16) error {
	// Stop and wait before deleting
	_ = service.stop(true)

	// Try to delete even if stop failed
	err := service.delete()
	if err != nil {
		return fmt.Errorf("failed to delete old service: %s", err)
	}

	// Wait until we can no longer open the old service.
	// Not very efficient but NotifyServiceStatusChange cannot be used with driver service.
	start := time.Now()
	timeLimit := time.Duration(30 * time.Second)
	for {
		handle, err := windows.OpenService(manager, &driverName[0], windows.SERVICE_ALL_ACCESS)
		if err != nil {
			break
		}
		_ = windows.CloseServiceHandle(handle)

		if time.Since(start) > timeLimit {
			return fmt.Errorf("time limit reached")
		}

		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *KextService) isValid() bool {
	return s != nil && s.handle != winInvalidHandleValue && s.handle != 0
}

func (s *KextService) isRunning() (bool, error) {
	if !s.isValid() {
		return false, fmt.Errorf("kext service not initialized")
	}
	var status windows.SERVICE_STATUS
	err := windows.QueryServiceStatus(s.handle, &status)
	if err != nil {
		return false, err
	}
	return status.CurrentState == windows.SERVICE_RUNNING, nil
}

func waitForServiceStatus(handle windows.Handle, neededStatus uint32, timeLimit time.Duration) (bool, error) {
	var status windows.SERVICE_STATUS
	status.CurrentState = windows.SERVICE_NO_CHANGE
	start := time.Now()
	for status.CurrentState != neededStatus {
		err := windows.QueryServiceStatus(handle, &status)
		if err != nil {
			return false, fmt.Errorf("failed while waiting for service to start: %w", err)
		}

		if time.Since(start) > timeLimit {
			return false, fmt.Errorf("time limit reached")
		}

		// Sleep for 1/10 of the wait hint, recommended time from microsoft
		time.Sleep(time.Duration((status.WaitHint / 10)) * time.Millisecond)
	}

	return true, nil
}

func (s *KextService) start(wait bool) error {
	if !s.isValid() {
		return fmt.Errorf("kext service not initialized")
	}

	// Start the service:
	err := windows.StartService(s.handle, 0, nil)

	if err != nil {
		err = windows.GetLastError()
		if err != windows.ERROR_SERVICE_ALREADY_RUNNING {
			// Failed to start service; clean-up:
			var status windows.SERVICE_STATUS
			_ = windows.ControlService(s.handle, windows.SERVICE_CONTROL_STOP, &status)
			_ = windows.DeleteService(s.handle)
			_ = windows.CloseServiceHandle(s.handle)
			s.handle = winInvalidHandleValue
			return err
		}
	}

	// Wait for service to start
	if wait {
		success, err := waitForServiceStatus(s.handle, windows.SERVICE_RUNNING, time.Duration(10*time.Second))
		if err != nil || !success {
			return fmt.Errorf("service did not start: %w", err)
		}
	}

	return nil
}

func (s *KextService) stop(wait bool) error {
	if !s.isValid() {
		return fmt.Errorf("kext service not initialized")
	}

	// Stop the service
	var status windows.SERVICE_STATUS
	err := windows.ControlService(s.handle, windows.SERVICE_CONTROL_STOP, &status)
	if err != nil {
		return fmt.Errorf("service failed to stop: %w", err)
	}

	// Wait for service to stop
	if wait {
		success, err := waitForServiceStatus(s.handle, windows.SERVICE_STOPPED, time.Duration(10*time.Second))
		if err != nil || !success {
			return fmt.Errorf("service did not stop: %w", err)
		}
	}

	return nil
}

func (s *KextService) delete() error {
	if !s.isValid() {
		return fmt.Errorf("kext service not initialized")
	}

	err := windows.DeleteService(s.handle)
	if err != nil {
		return fmt.Errorf("failed to delete service: %s", err)
	}

	// Service wont be deleted until all handles are closed.
	err = windows.CloseServiceHandle(s.handle)
	if err != nil {
		return fmt.Errorf("failed to close service handle: %s", err)
	}

	s.handle = winInvalidHandleValue
	return nil
}
