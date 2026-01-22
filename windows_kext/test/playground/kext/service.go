//go:build windows
// +build windows

package kext

import (
	"fmt"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// KextService manages the kernel driver service
type KextService struct {
	handle     windows.Handle
	driverName string
}

// NewKextService creates or opens the kernel driver service
func NewKextService(driverName string, driverPath string) (*KextService, error) {
	if driverName == "" {
		driverName = defaultDriverName
	}

	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS)
	if err != nil {
		return nil, fmt.Errorf("failed to open service manager: %w", err)
	}
	defer windows.CloseServiceHandle(manager)

	driverNameU16, err := syscall.UTF16FromString(driverName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver name to UTF16: %w", err)
	}

	// Check if there is an existing service
	service, err := windows.OpenService(manager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
	if err == nil {
		// Old service found - stop and delete it
		fmt.Println("[kext] Old driver service found, cleaning up...")
		oldService := &KextService{handle: service, driverName: driverName}
		if err := oldService.Stop(true); err != nil {
			return nil, fmt.Errorf("failed to stop old service: %w", err)
		}
		if err := oldService.Delete(); err != nil {
			// Ignore "marked for deletion" error - service will be cleaned up
			if !strings.Contains(err.Error(), "marked for deletion") {
				return nil, fmt.Errorf("failed to delete old service: %w", err)
			}
			fmt.Println("[kext] Service marked for deletion, waiting...")
		}
		if err := oldService.WaitUntilDeleted(manager); err != nil {
			return nil, fmt.Errorf("failed waiting for old service deletion: %w", err)
		}
		fmt.Println("[kext] Old driver service deleted successfully")
	}

	driverPathU16, err := syscall.UTF16FromString(driverPath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver path to UTF16: %w", err)
	}

	// Create the service
	service, err = windows.CreateService(
		manager,
		&driverNameU16[0],
		&driverNameU16[0],
		windows.SERVICE_ALL_ACCESS,
		windows.SERVICE_KERNEL_DRIVER,
		windows.SERVICE_DEMAND_START,
		windows.SERVICE_ERROR_NORMAL,
		&driverPathU16[0],
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return &KextService{handle: service, driverName: driverName}, nil
}

func (s *KextService) isValid() bool {
	return s != nil && s.handle != windows.InvalidHandle && s.handle != 0
}

// IsRunning checks if the service is currently running
func (s *KextService) IsRunning() (bool, error) {
	if !s.isValid() {
		return false, ErrServiceNotValid
	}
	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(s.handle, &status); err != nil {
		return false, err
	}
	return status.CurrentState == windows.SERVICE_RUNNING, nil
}

func (s *KextService) waitForStatus(neededStatus uint32, timeout time.Duration) error {
	var status windows.SERVICE_STATUS
	start := time.Now()
	for {
		if err := windows.QueryServiceStatus(s.handle, &status); err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
		if status.CurrentState == neededStatus {
			return nil
		}
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for service status %d", neededStatus)
		}
		time.Sleep(time.Duration(status.WaitHint/10) * time.Millisecond)
	}
}

// Start starts the driver service
func (s *KextService) Start(wait bool) error {
	if !s.isValid() {
		return ErrServiceNotValid
	}

	if err := windows.StartService(s.handle, 0, nil); err != nil {
		if err != windows.ERROR_SERVICE_ALREADY_RUNNING {
			return fmt.Errorf("failed to start service: %w", err)
		}
	}

	if wait {
		if err := s.waitForStatus(windows.SERVICE_RUNNING, stopServiceTimeoutDuration); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops the driver service
func (s *KextService) Stop(wait bool) error {
	fmt.Println("[kext] Stopping driver service...")
	if !s.isValid() {
		return ErrServiceNotValid
	}

	fmt.Println("[kext] Sending stop control to driver service...")
	var status windows.SERVICE_STATUS
	if err := windows.ControlService(s.handle, windows.SERVICE_CONTROL_STOP, &status); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	if wait {
		fmt.Println("[kext] Waiting for driver service to stop...")
		if err := s.waitForStatus(windows.SERVICE_STOPPED, 10*time.Second); err != nil {
			return err
		}
	}
	fmt.Println("[kext] Driver service stopped successfully")
	return nil
}

// Delete deletes the driver service
func (s *KextService) Delete() error {
	if !s.isValid() {
		return ErrServiceNotValid
	}

	if err := windows.DeleteService(s.handle); err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	if err := windows.CloseServiceHandle(s.handle); err != nil {
		return fmt.Errorf("failed to close service handle: %w", err)
	}

	s.handle = windows.InvalidHandle
	return nil
}

// WaitUntilDeleted waits until the service is fully deleted
func (s *KextService) WaitUntilDeleted(manager windows.Handle) error {
	driverNameU16, err := syscall.UTF16FromString(s.driverName)
	if err != nil {
		return fmt.Errorf("failed to convert driver name: %w", err)
	}

	timeout := 30 * time.Second
	start := time.Now()
	for {
		handle, err := windows.OpenService(manager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
		if err != nil {
			return nil // Service no longer exists
		}
		_ = windows.CloseServiceHandle(handle)

		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for service deletion")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// OpenFile opens a communication channel with the driver
func (s *KextService) OpenFile(readBufferSize int) (*KextFile, error) {
	if !s.isValid() {
		return nil, ErrServiceNotValid
	}

	devicePath := `\\.\` + s.driverName
	devicePathU16, err := syscall.UTF16FromString(devicePath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert device path: %w", err)
	}

	handle, err := windows.CreateFile(
		&devicePathU16[0],
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	return &KextFile{
		handle: handle,
		buffer: make([]byte, readBufferSize),
	}, nil
}

// Close closes the service handle
func (s *KextService) Close() error {
	if !s.isValid() {
		return nil
	}
	err := windows.CloseServiceHandle(s.handle)
	s.handle = windows.InvalidHandle
	return err
}
