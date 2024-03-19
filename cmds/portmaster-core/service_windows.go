package main

import (
	"errors"
	"flag"
	"log"
	"time"

	"github.com/safing/portbase/modules"
	"golang.org/x/sys/windows/svc"
)

var runAsService bool

func init() {
	flag.BoolVar(&runAsService, "service", false, "(windows only) run portmaster-core as a service")
}

func shouldRunService() bool {
	return runAsService
}

const serviceName = "PortmasterCore"

type windowsService struct{}

func (ws *windowsService) Execute(args []string, changeRequests <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	err := modules.Start()
	if err != nil {
		// Immediately return for a clean exit.
		if errors.Is(err, modules.ErrCleanExit) {
			// send stopped status
			changes <- svc.Status{State: svc.Stopped}
			// wait a little for the status to reach Windows
			time.Sleep(100 * time.Millisecond)
			return false, 0
		}

		// Trigger shutdown and wait for it to complete.
		_ = modules.Shutdown()

	} else {

		// Modules are running.
		changes <- svc.Status{State: svc.Running}

		// Listen for updates
	service:
		for {
			select {
			case c := <-changeRequests:
				switch c.Cmd {
				case svc.Interrogate:
					changes <- c.CurrentStatus
				case svc.Stop, svc.Shutdown:
					modules.Shutdown()
				default:
					log.Printf("unexpected control request: #%d\n", c)
				}
			case <-modules.ShuttingDown():
				{
					changes <- svc.Status{State: svc.StopPending}
					break service
				}
			}

		}
	}

	// Wait for modules to shutdown.
	if modules.GetExitStatusCode() != 0 {
		ssec = true // this error is specific to this service (ie. custom)
		errno = 1   // generic error, check logs / windows events
	}

	// send stopped status
	changes <- svc.Status{State: svc.Stopped}
	// wait a little for the status to reach Windows
	time.Sleep(100 * time.Millisecond)

	return ssec, errno
}

func runService() int {
	_ = svc.Run(serviceName, &windowsService{})
	return 0
}
