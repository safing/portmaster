package main

// Based on the offical Go examples from
// https://github.com/golang/sys/blob/master/windows/svc/example
// by The Go Authors.
// Original LICENSE (sha256sum: 2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067) can be found in vendor/pkg cache directory.

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

var (
	runCoreService = &cobra.Command{
		Use:   "core-service",
		Short: "Run the Portmaster Core as a Windows Service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, &Options{
				Identifier:        "core/portmaster-core",
				AllowDownload:     true,
				AllowHidingWindow: true,
			})
		},
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
			UnknownFlags: true,
		},
	}

	// helpers for execution
	runError   chan error
	runWrapper func() error

	// eventlog
	eventlogger *eventlog.Log
)

func init() {
	runCmd.AddCommand(runCoreService)
}

const serviceName = "PortmasterCore"

type windowsService struct{}

func (ws *windowsService) Execute(args []string, changeRequests <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// start logic
	var runError chan error
	go func() {
		runError <- runWrapper()
	}()

	// poll for start completion
	started := make(chan struct{})
	go func() {
		for {
			time.Sleep(10 * time.Millisecond)
			if childIsRunning.IsSet() {
				close(started)
				return
			}
		}
	}()

	// wait for start
	select {
	case err := <-runError:
		// TODO: log error to windows
		fmt.Printf("%s start error: %s", logPrefix, err)
		eventlogger.Error(4, fmt.Sprintf("failed to start Portmaster Core: %s", err))
		changes <- svc.Status{State: svc.Stopped}
		return false, 1
	case <-started:
		// give some more time for enabling packet interception
		time.Sleep(500 * time.Millisecond)
		changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
		fmt.Printf("%s startup complete, entered service running state\n", logPrefix)
	}

	// wait for change requests
serviceLoop:
	for {
		select {
		case <-shuttingDown:
			break serviceLoop
		case c := <-changeRequests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				initiateShutdown()
				break serviceLoop
			default:
				fmt.Printf("%s unexpected control request: #%d\n", logPrefix, c)
			}
		}
	}

	// signal that we are shutting down
	changes <- svc.Status{State: svc.StopPending}
	// wait for program to exit
	<-programEnded
	// signal shutdown complete
	changes <- svc.Status{State: svc.Stopped}
	return
}

func runService(cmd *cobra.Command, opts *Options) error {
	// set run wrapper
	runWrapper = func() error {
		return run(cmd, opts)
	}

	// check if we are running interactively
	isDebug, err := svc.IsAnInteractiveSession()
	if err != nil {
		return fmt.Errorf("could not determine if running interactively: %s", err)
	}

	// open eventlog
	// TODO: do something useful with eventlog
	elog, err := eventlog.Open(serviceName)
	if err != nil {
		return fmt.Errorf("failed to open eventlog: %s", err)
	}
	defer elog.Close()
	eventlogger = elog
	elog.Info(1, fmt.Sprintf("starting %s service", serviceName))

	// select run method bas
	run := svc.Run
	if isDebug {
		fmt.Printf("%s WARNING: running interactively, switching to debug execution (no real service).\n", logPrefix)
		run = debug.Run
	}
	// run
	err = run(serviceName, &windowsService{})
	if err != nil {
		elog.Error(3, fmt.Sprintf("%s service failed: %v", serviceName, err))
		return fmt.Errorf("failed to start service: %s", err)
	}
	elog.Info(2, fmt.Sprintf("%s service stopped", serviceName))

	return nil
}
