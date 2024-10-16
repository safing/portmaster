//go:build windows
// +build windows

package kextinterface

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/safing/portmaster/base/log"
	"golang.org/x/sys/windows"
)

var (
	//go:embed version.txt
	versionTxt string

	// 4 byte version of the Kext interface
	InterfaceVersion = func() (v [4]byte) {
		// Parse version from file "version.txt". Expected format: [0, 1, 2, 3]
		s := strings.TrimSpace(versionTxt)
		s = strings.TrimPrefix(s, "[")
		s = strings.TrimSuffix(s, "]")
		str_ver := strings.Split(s, ",")
		for i := range v {
			n, err := strconv.Atoi(strings.TrimSpace(str_ver[i]))
			if err != nil {
				panic(err)
			}
			v[i] = byte(n)
		}
		return
	}()
)

const (
	winInvalidHandleValue      = windows.InvalidHandle
	stopServiceTimeoutDuration = time.Duration(30 * time.Second)
)

type KextService struct {
	handle     windows.Handle
	driverName string
}

func (s *KextService) isValid() bool {
	return s != nil && s.handle != windows.InvalidHandle && s.handle != 0
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

func (s *KextService) waitForServiceStatus(neededStatus uint32, timeLimit time.Duration) (bool, error) {
	var status windows.SERVICE_STATUS
	status.CurrentState = windows.SERVICE_NO_CHANGE
	start := time.Now()
	for status.CurrentState != neededStatus {
		err := windows.QueryServiceStatus(s.handle, &status)
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

func (s *KextService) Start(wait bool) error {
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
			s.handle = windows.InvalidHandle
			return err
		}
	}

	// Wait for service to start
	if wait {
		success, err := s.waitForServiceStatus(windows.SERVICE_RUNNING, stopServiceTimeoutDuration)
		if err != nil || !success {
			return fmt.Errorf("service did not start: %w", err)
		}
	}

	return nil
}

func (s *KextService) GetHandle() windows.Handle {
	return s.handle
}

func (s *KextService) Stop(wait bool) error {
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
		success, err := s.waitForServiceStatus(windows.SERVICE_STOPPED, time.Duration(10*time.Second))
		if err != nil || !success {
			return fmt.Errorf("service did not stop: %w", err)
		}
	}

	return nil
}

func (s *KextService) Delete() error {
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

	s.handle = windows.InvalidHandle
	return nil
}

func (s *KextService) WaitUntilDeleted(serviceManager windows.Handle) error {
	driverNameU16, err := syscall.UTF16FromString(s.driverName)
	if err != nil {
		return fmt.Errorf("failed to convert driver name to UTF16 string: %w", err)
	}
	// Wait until we can no longer open the old service.
	// Not very efficient but NotifyServiceStatusChange cannot be used with driver service.
	start := time.Now()
	timeLimit := time.Duration(30 * time.Second)
	for {
		handle, err := windows.OpenService(serviceManager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
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

func (s *KextService) OpenFile(readBufferSize int) (*KextFile, error) {
	if !s.isValid() {
		return nil, fmt.Errorf("invalid kext object")
	}

	driverNameU16, err := syscall.UTF16FromString(`\\.\` + s.driverName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver driverName to UTF16 string %w", err)
	}

	handle, err := windows.CreateFile(&driverNameU16[0], windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}

	return &KextFile{handle: handle, buffer: make([]byte, readBufferSize)}, nil
}

func CreateKextService(driverName string, driverPath string) (*KextService, error) {
	// Open the service manager:
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS)
	if err != nil {
		return nil, fmt.Errorf("failed to open service manager: %d", err)
	}
	defer windows.CloseServiceHandle(manager)

	driverNameU16, err := syscall.UTF16FromString(driverName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert driver name to UTF16 string: %w", err)
	}

	// Check if there is an old service.
	service, err := windows.OpenService(manager, &driverNameU16[0], windows.SERVICE_ALL_ACCESS)
	if err == nil {
		log.Warning("kext: old driver service was found")
		oldService := &KextService{handle: service, driverName: driverName}
		oldService.Stop(true)
		err = oldService.Delete()
		if err != nil {
			return nil, err
		}
		err := oldService.WaitUntilDeleted(manager)
		if err != nil {
			return nil, err
		}

		service = windows.InvalidHandle
		log.Warning("kext: old driver service was deleted successfully")
	}

	driverPathU16, err := syscall.UTF16FromString(driverPath)

	// Create the service
	service, err = windows.CreateService(manager, &driverNameU16[0], &driverNameU16[0], windows.SERVICE_ALL_ACCESS, windows.SERVICE_KERNEL_DRIVER, windows.SERVICE_DEMAND_START, windows.SERVICE_ERROR_NORMAL, &driverPathU16[0], nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return &KextService{handle: service, driverName: driverName}, nil
}
