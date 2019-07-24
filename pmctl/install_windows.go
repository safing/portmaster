package main

// Based on the offical Go examples from
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

func getExePath() (string, error) {
	// get own filepath
	prog := os.Args[0]
	p, err := filepath.Abs(prog)
	if err != nil {
		return "", err
	}
	// check if the path is valid
	fi, err := os.Stat(p)
	if err == nil {
		if !fi.Mode().IsDir() {
			return p, nil
		}
		err = fmt.Errorf("%s is directory", p)
	}
	// check if we have a .exe extension, add and check if not
	if filepath.Ext(p) == "" {
		p += ".exe"
		fi, err := os.Stat(p)
		if err == nil {
			if !fi.Mode().IsDir() {
				return p, nil
			}
			err = fmt.Errorf("%s is directory", p)
		}
	}
	return "", err
}

func getServiceExecCommand(exePath string) []string {
	return []string{
		windows.EscapeArg(exePath),
		"run",
		"core-service",
		"--db",
		windows.EscapeArg(*databaseRootDir),
		"--input-signals",
	}
}

func getServiceConfig(exePath string) mgr.Config {
	return mgr.Config{
		ServiceType:    windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:      mgr.StartAutomatic,
		ErrorControl:   mgr.ErrorNormal,
		BinaryPathName: strings.Join(getServiceExecCommand(exePath), " "),
		DisplayName:    "Portmaster Core",
		Description:    "Portmaster Application Firewall - Core Service",
	}
}

func getRecoveryActions() (recoveryActions []mgr.RecoveryAction, resetPeriod uint32) {
	return []mgr.RecoveryAction{
		//mgr.RecoveryAction{
		//	Type:  mgr.ServiceRestart, // one of NoAction, ComputerReboot, ServiceRestart or RunCommand
		//	Delay: 1 * time.Minute,    // the time to wait before performing the specified action
		//},
		// mgr.RecoveryAction{
		// 	Type:  mgr.ServiceRestart, // one of NoAction, ComputerReboot, ServiceRestart or RunCommand
		// 	Delay: 1 * time.Minute,    // the time to wait before performing the specified action
		// },
		mgr.RecoveryAction{
			Type:  mgr.ServiceRestart, // one of NoAction, ComputerReboot, ServiceRestart or RunCommand
			Delay: 1 * time.Minute,    // the time to wait before performing the specified action
		},
	}, 86400
}

func installWindowsService(cmd *cobra.Command, args []string) error {
	// get exe path
	exePath, err := getExePath()
	if err != nil {
		return fmt.Errorf("failed to get exe path: %s", err)
	}

	// connect to Windows service manager
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %s", err)
	}
	defer m.Disconnect()

	// open service
	created := false
	s, err := m.OpenService(serviceName)
	if err != nil {
		// create service
		cmd := getServiceExecCommand(exePath)
		s, err = m.CreateService(serviceName, cmd[0], getServiceConfig(exePath), cmd[1:]...)
		if err != nil {
			return fmt.Errorf("failed to create service: %s", err)
		}
		defer s.Close()
		created = true
	} else {
		// update service
		s.UpdateConfig(getServiceConfig(exePath))
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
	defer m.Disconnect()

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
