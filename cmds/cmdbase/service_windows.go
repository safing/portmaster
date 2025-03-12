package cmdbase

// Based on the official Go examples from
// https://github.com/golang/sys/blob/master/windows/svc/example
// by The Go Authors.
// Original LICENSE (sha256sum: 2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067) can be found in vendor/pkg cache directory.

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"

	"github.com/safing/portmaster/base/log"
)

const serviceName = "PortmasterCore"

type WindowsSystemService struct {
	instance ServiceInstance
}

func NewSystemService(instance ServiceInstance) *WindowsSystemService {
	return &WindowsSystemService{instance: instance}
}

func (s *WindowsSystemService) Run() {
	svcRun := svc.Run

	// Check if we are running interactively.
	isService, err := svc.IsWindowsService()
	switch {
	case err != nil:
		slog.Warn("failed to determine if running interactively", "err", err)
		slog.Warn("continuing without service integration (no real service)")
		svcRun = debug.Run

	case !isService:
		slog.Warn("running interactively, switching to debug execution (no real service)")
		svcRun = debug.Run
	}

	// Run service client.
	err = svcRun(serviceName, s)
	if err != nil {
		slog.Error("service execution failed", "err", err)
		os.Exit(1)
	}

	// Execution continues in s.Execute().
}

func (s *WindowsSystemService) Execute(args []string, changeRequests <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	// Tell service manager we are starting.
	changes <- svc.Status{State: svc.StartPending}

	// Start instance.
	err := s.instance.Start()
	if err != nil {
		fmt.Printf("failed to start: %s\n", err)

		// Print stack on start failure, if enabled.
		if PrintStackOnExit {
			printStackTo(log.GlobalWriter, "PRINTING STACK ON START FAILURE")
		}

		// Notify service manager we stopped again.
		changes <- svc.Status{State: svc.Stopped}

		// Relay exit code to service manager.
		return false, 1
	}

	// Tell service manager we are up and running!
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	// Subscribe to signals.
	// Docs: https://pkg.go.dev/os/signal?GOOS=windows
	signalCh := make(chan os.Signal, 4)
	signal.Notify(
		signalCh,

		// Windows ^C (Control-C) or ^BREAK (Control-Break).
		// Completely prevents kill.
		os.Interrupt,

		// Windows CTRL_CLOSE_EVENT, CTRL_LOGOFF_EVENT or CTRL_SHUTDOWN_EVENT.
		// Does not prevent kill, but gives a little time to stop service.
		syscall.SIGTERM,
	)

	// Wait for shutdown signal.
waitSignal:
	for {
		select {
		case sig := <-signalCh:
			// Trigger shutdown.
			fmt.Printf(" <SIGNAL: %v>\n", sig) // CLI output.
			slog.Warn("received stop signal", "signal", sig)
			break waitSignal

		case c := <-changeRequests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus

			case svc.Stop, svc.Shutdown:
				fmt.Printf(" <SERVICE CMD: %v>\n", serviceCmdName(c.Cmd)) // CLI output.
				slog.Warn("received service shutdown command", "cmd", c.Cmd)
				break waitSignal

			default:
				slog.Error("unexpected service control request", "cmd", serviceCmdName(c.Cmd))
			}

		case <-s.instance.ShuttingDown():
			break waitSignal
		}
	}

	// Trigger shutdown.
	s.instance.Shutdown()

	// Notify the service host that service is in shutting down state.
	changes <- svc.Status{State: svc.StopPending}

	// Wait for shutdown to finish.
	// Catch signals during shutdown.
	// Force exit after 5 interrupts.
	forceCnt := 5
waitShutdown:
	for {
		select {
		case <-s.instance.ShutdownComplete():
			break waitShutdown

		case sig := <-signalCh:
			forceCnt--
			if forceCnt > 0 {
				fmt.Printf(" <SIGNAL: %s> but already shutting down - %d more to force\n", sig, forceCnt)
			} else {
				printStackTo(log.GlobalWriter, "PRINTING STACK ON FORCED EXIT")
				os.Exit(1)
			}

		case c := <-changeRequests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus

			case svc.Stop, svc.Shutdown:
				forceCnt--
				if forceCnt > 0 {
					fmt.Printf(" <SERVICE CMD: %v> but already shutting down - %d more to force\n", serviceCmdName(c.Cmd), forceCnt)
				} else {
					printStackTo(log.GlobalWriter, "PRINTING STACK ON FORCED EXIT")
					os.Exit(1)
				}

			default:
				slog.Error("unexpected service control request", "cmd", serviceCmdName(c.Cmd))
			}
		}
	}

	// Notify service manager.
	changes <- svc.Status{State: svc.Stopped}

	return false, 0
}

func (s *WindowsSystemService) IsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

func (s *WindowsSystemService) RestartService() error {
	// Script that wait for portmaster service status to change to stop
	// and then sends a start command for the same service.
	command := `
$serviceName = "PortmasterCore"
while ((Get-Service -Name $serviceName).Status -ne 'Stopped') {
    Start-Sleep -Seconds 1
}
sc.exe start $serviceName`

	// Create the command to execute the PowerShell script
	cmd := exec.Command("powershell.exe", "-Command", command)
	// Start the command. The script will continue even after the parent process exits.
	err := cmd.Start()
	if err != nil {
		return err
	}

	return nil
}

func serviceCmdName(cmd svc.Cmd) string {
	switch cmd {
	case svc.Stop:
		return "Stop"
	case svc.Pause:
		return "Pause"
	case svc.Continue:
		return "Continue"
	case svc.Interrogate:
		return "Interrogate"
	case svc.Shutdown:
		return "Shutdown"
	case svc.ParamChange:
		return "ParamChange"
	case svc.NetBindAdd:
		return "NetBindAdd"
	case svc.NetBindRemove:
		return "NetBindRemove"
	case svc.NetBindEnable:
		return "NetBindEnable"
	case svc.NetBindDisable:
		return "NetBindDisable"
	case svc.DeviceEvent:
		return "DeviceEvent"
	case svc.HardwareProfileChange:
		return "HardwareProfileChange"
	case svc.PowerEvent:
		return "PowerEvent"
	case svc.SessionChange:
		return "SessionChange"
	case svc.PreShutdown:
		return "PreShutdown"
	default:
		return "Unknown Command"
	}
}
