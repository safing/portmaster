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
	"sync"
	"syscall"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

var (
	// wait groups
	runWg    sync.WaitGroup
	finishWg sync.WaitGroup

	defaultRestartCommand = exec.Command("sc.exe", "restart", "PortmasterCore")
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
			changes <- svc.Status{State: svc.StopPending}
			break service
		case c := <-changeRequests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				ws.instance.Shutdown()
			default:
				log.Errorf("unexpected control request: #%d", c)
			}
		}
	}

	// wait until everything else is finished
	// finishWg.Wait()

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

	runWg.Add(1)

	// run service client
	go func() {
		sErr := svcRun(serviceName, &windowsService{
			instance: instance,
		})
		if sErr != nil {
			log.Infof("shuting down service with error: %s", sErr)
		} else {
			log.Infof("shuting down service")
		}
		runWg.Done()
	}()

	// finishWg.Add(1)
	// run service
	// go func() {
	// 	// run slightly delayed
	// 	time.Sleep(250 * time.Millisecond)

	// 	if err != nil {
	// 		fmt.Printf("instance start failed: %s\n", err)

	// 		// Print stack on start failure, if enabled.
	// 		if printStackOnExit {
	// 			printStackTo(os.Stdout, "PRINTING STACK ON START FAILURE")
	// 		}

	// 	}
	// 	runWg.Done()
	// 	finishWg.Done()
	// }()

	runWg.Wait()

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
