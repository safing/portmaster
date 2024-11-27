package main

// Based on the official Go examples from
// https://github.com/golang/sys/blob/master/windows/svc/example
// by The Go Authors.
// Original LICENSE (sha256sum: 2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067) can be found in vendor/pkg cache directory.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.AddCommand(installService)

	rootCmd.AddCommand(uninstallCmd)
	uninstallCmd.AddCommand(uninstallService)
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install system integrations",
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall system integrations",
}

var installService = &cobra.Command{
	Use:   "core-service",
	Short: "Install Portmaster Core Windows Service",
	RunE:  installWindowsService,
}

var uninstallService = &cobra.Command{
	Use:   "core-service",
	Short: "Uninstall Portmaster Core Windows Service",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// non-nil dummy to override db flag requirement
		return nil
	},
	RunE: uninstallWindowsService,
}

func getAbsBinaryPath() (string, error) {
	p, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", err
	}

	return p, nil
}

func getServiceExecCommand(exePath string, escape bool) []string {
	return []string{
		maybeEscape(exePath, escape),
		"core-service",
		"--data",
		maybeEscape(dataRoot.Path, escape),
		"--input-signals",
	}
}

func maybeEscape(s string, escape bool) string {
	if escape {
		return windows.EscapeArg(s)
	}
	return s
}

func getServiceConfig(exePath string) mgr.Config {
	return mgr.Config{
		ServiceType:    windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:      mgr.StartAutomatic,
		ErrorControl:   mgr.ErrorNormal,
		BinaryPathName: strings.Join(getServiceExecCommand(exePath, true), " "),
		DisplayName:    "Portmaster Core",
		Description:    "Portmaster Application Firewall - Core Service",
	}
}

func getRecoveryActions() (recoveryActions []mgr.RecoveryAction, resetPeriod uint32) {
	return []mgr.RecoveryAction{
		{
			Type:  mgr.ServiceRestart, // one of NoAction, ComputerReboot, ServiceRestart or RunCommand
			Delay: 1 * time.Minute,    // the time to wait before performing the specified action
		},
	}, 86400
}

func installWindowsService(cmd *cobra.Command, args []string) error {
	// get exe path
	exePath, err := getAbsBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get exe path: %s", err)
	}

	// connect to Windows service manager
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %s", err)
	}
	defer m.Disconnect() //nolint:errcheck // TODO

	// open service
	created := false
	s, err := m.OpenService(serviceName)
	if err != nil {
		// create service
		cmd := getServiceExecCommand(exePath, false)
		s, err = m.CreateService(serviceName, cmd[0], getServiceConfig(exePath), cmd[1:]...)
		if err != nil {
			return fmt.Errorf("failed to create service: %s", err)
		}
		defer s.Close()
		created = true
	} else {
		// update service
		err = s.UpdateConfig(getServiceConfig(exePath))
		if err != nil {
			return fmt.Errorf("failed to update service: %s", err)
		}
		defer s.Close()
	}

	// update recovery actions
	err = s.SetRecoveryActions(getRecoveryActions())
	if err != nil {
		return fmt.Errorf("failed to update recovery actions: %s", err)
	}

	if created {
		log.Printf("created service %s\n", serviceName)
	} else {
		log.Printf("updated service %s\n", serviceName)
	}

	return nil
}

func uninstallWindowsService(cmd *cobra.Command, args []string) error {
	// connect to Windows service manager
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect() //nolint:errcheck // we don't care if we failed to disconnect from the service manager, we're quitting anyway.

	// open service
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		log.Printf("failed to stop service: %s\n", err)
	}

	// delete service
	err = s.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete service: %s", err)
	}

	log.Printf("uninstalled service %s\n", serviceName)
	return nil
}
