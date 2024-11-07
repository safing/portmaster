package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	processInfo "github.com/shirou/gopsutil/process"
	"github.com/spf13/cobra"

	"github.com/safing/portmaster/service"
)

type LinuxSystemService struct {
	instance *service.Instance
}

func NewSystemService(instance *service.Instance) *LinuxSystemService {
	return &LinuxSystemService{instance: instance}
}

func (s *LinuxSystemService) Run() {
	// Start instance.
	err := s.instance.Start()
	if err != nil {
		slog.Error("failed to start", "err", err)

		// Print stack on start failure, if enabled.
		if printStackOnExit {
			printStackTo(os.Stderr, "PRINTING STACK ON START FAILURE")
		}

		os.Exit(1)
	}

	// Subscribe to signals.
	signalCh := make(chan os.Signal, 1)
	signal.Notify(
		signalCh,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGUSR1,
	)

	// Wait for shutdown signal.
wait:
	for {
		select {
		case sig := <-signalCh:
			// Only print and continue to wait if SIGUSR1
			if sig == syscall.SIGUSR1 {
				printStackTo(os.Stdout, "PRINTING STACK ON REQUEST")
				continue wait
			} else {
				// Trigger shutdown.
				fmt.Printf(" <SIGNAL: %v>", sig) // CLI output.
				slog.Warn("received stop signal", "signal", sig)
				s.instance.Shutdown()
				break wait
			}
		case <-s.instance.ShuttingDown():
			break wait
		}
	}

	// Wait for shutdown to finish.

	// Catch signals during shutdown.
	// Force exit after 5 interrupts.
	forceCnt := 5
	for {
		select {
		case <-s.instance.ShutdownComplete():
			return
		case sig := <-signalCh:
			if sig != syscall.SIGUSR1 {
				forceCnt--
				if forceCnt > 0 {
					fmt.Printf(" <SIGNAL: %s> again, but already shutting down - %d more to force\n", sig, forceCnt)
				} else {
					printStackTo(os.Stderr, "PRINTING STACK ON FORCED EXIT")
					os.Exit(1)
				}
			}
		}
	}
}

func (s *LinuxSystemService) RestartService() error {
	// Check if user defined custom command for restarting the service.
	restartCommand, exists := os.LookupEnv("PORTMASTER_RESTART_COMMAND")

	// Run the service restart
	var cmd *exec.Cmd
	if exists && restartCommand != "" {
		slog.Debug("running custom restart command", "command", restartCommand)
		cmd = exec.Command("sh", "-c", restartCommand)
	} else {
		cmd = exec.Command("systemctl", "restart", "portmaster")
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed run restart command: %w", err)
	}
	return nil
}

func (s *LinuxSystemService) IsService() bool {
	// Get own process ID
	pid := os.Getpid()

	// Get parent process ID.
	currentProcess, err := processInfo.NewProcess(int32(pid)) //nolint:gosec
	if err != nil {
		return false
	}
	ppid, err := currentProcess.Ppid()
	if err != nil {
		return false
	}

	// Check if the parent process ID is 1 == init system
	return ppid == 1
}

func runPlatformSpecifics(cmd *cobra.Command, args []string) {
	// If recover-iptables flag is set, run the recover-iptables command.
	// This is for backwards compatibility
	if recoverIPTables {
		exitCode := 0
		err := recover(cmd, args)
		if err != nil {
			fmt.Printf("failed: %s", err)
			exitCode = 1
		}

		os.Exit(exitCode)
	}
}
