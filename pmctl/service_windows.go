package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
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
	var started chan struct{}
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
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
		return false, 1
	case <-started:
		// give some more time for enabling packet interception
		time.Sleep(500 * time.Millisecond)
		changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
		fmt.Printf("%s startup complete, entered service running state", logPrefix)
	}

	// wait for change requests
	for {
		select {
		case <-shuttingDown:
			// signal that we are shutting down
			changes <- svc.Status{State: svc.StopPending}
			// wait for program to exit
			<-programEnded
			return
		case c := <-changeRequests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				initiateShutdown()
				// wait for program to exit
				<-programEnded
				return
			default:
				fmt.Printf("%s unexpected control request: #%d", logPrefix, c)
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func runService(cmd *cobra.Command, opts *Options) error {
	// set run wrapper
	runWrapper = func() error {
		return run(cmd, opts)
	}

	// open eventlog
	// TODO: do something useful with eventlog
	elog, err := eventlog.Open(serviceName)
	if err != nil {
		return fmt.Errorf("failed to open eventlog: %s", err)
	}
	defer elog.Close()
	elog.Info(1, fmt.Sprintf("starting %s service", serviceName))

	err = svc.Run(serviceName, &windowsService{})
	if err != nil {
		elog.Error(3, fmt.Sprintf("%s service failed: %v", serviceName, err))
		return fmt.Errorf("failed to start service: %s", err)
	}
	elog.Info(2, fmt.Sprintf("%s service stopped", serviceName))

	return nil
}
