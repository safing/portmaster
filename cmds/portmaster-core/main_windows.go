package main

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
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

const serviceName = "PortmasterCore"

type windowsService struct {
	instance *service.Instance
}

func (ws *windowsService) Execute(args []string, changeRequests <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	ws.instance.Start()
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

service:
	for {
		select {
		case <-ws.instance.Stopped():
			log.Infof("instance stopped")
			break service
		case c := <-changeRequests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Debugf("received shutdown command")
				changes <- svc.Status{State: svc.StopPending}
				ws.instance.Shutdown()
			default:
				log.Errorf("unexpected control request: #%d", c)
			}
		}
	}

	log.Shutdown()

	// send stopped status
	changes <- svc.Status{State: svc.Stopped}
	// wait a little for the status to reach Windows
	time.Sleep(100 * time.Millisecond)

	return ssec, errno
}

func run(instance *service.Instance) error {
	log.SetLogLevel(log.WarningLevel)
	_ = log.Start()

	// check if we are running interactively
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("could not determine if running interactively: %s", err)
	}

	// select service run type
	svcRun := svc.Run
	if !isService {
		log.Warningf("running interactively, switching to debug execution (no real service).")
		svcRun = debug.Run
		go registerSignalHandler(instance)
	}

	// run service client
	sErr := svcRun(serviceName, &windowsService{
		instance: instance,
	})
	if sErr != nil {
		fmt.Printf("shuting down service with error: %s", sErr)
	} else {
		fmt.Printf("shuting down service")
	}

	// Check if restart was trigger and send start service command if true.
	if isRunningAsService() && instance.ShouldRestart {
		_ = runServiceRestart()
	}

	return err
}

func registerSignalHandler(instance *service.Instance) {
	// Wait for signal.
	signalCh := make(chan os.Signal, 1)
	if enableInputSignals {
		go inputSignals(signalCh)
	}
	signal.Notify(
		signalCh,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		sigUSR1,
	)

	select {
	case sig := <-signalCh:
		// Only print and continue to wait if SIGUSR1
		if sig == sigUSR1 {
			printStackTo(os.Stderr, "PRINTING STACK ON REQUEST")
		} else {
			fmt.Println(" <INTERRUPT>") // CLI output.
			slog.Warn("program was interrupted, stopping")
			instance.Shutdown()
		}
	}

	// Catch signals during shutdown.
	// Rapid unplanned disassembly after 5 interrupts.
	go func() {
		forceCnt := 5
		for {
			<-signalCh
			forceCnt--
			if forceCnt > 0 {
				fmt.Printf(" <INTERRUPT> again, but already shutting down - %d more to force\n", forceCnt)
			} else {
				printStackTo(os.Stderr, "PRINTING STACK ON FORCED EXIT")
				os.Exit(1)
			}
		}
	}()
}

func isRunningAsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

func runServiceRestart() error {
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
