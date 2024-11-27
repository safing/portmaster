package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"
)

var (
	printStackOnExit   bool
	enableInputSignals bool

	sigUSR1 = syscall.Signal(0xa) // dummy for windows
)

func init() {
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
	flag.BoolVar(&enableInputSignals, "input-signals", false, "emulate signals using stdin")
}

func main() {
	flag.Parse()

	// set information
	info.Set("Portmaster", "", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("Portmaster Core (%s %s)", runtime.GOOS, runtime.GOARCH)

	// enable SPN client mode
	conf.EnableClient(true)
	conf.EnableIntegration(true)

	// Create instance.
	var execCmdLine bool
	instance, err := service.New(&service.ServiceConfig{})
	switch {
	case err == nil:
		// Continue
	case errors.Is(err, mgr.ErrExecuteCmdLineOp):
		execCmdLine = true
	default:
		fmt.Printf("error creating an instance: %s\n", err)
		os.Exit(2)
	}

	// Execute command line operation, if requested or available.
	switch {
	case !execCmdLine:
		// Run service.
	case instance.CommandLineOperation == nil:
		fmt.Println("command line operation execution requested, but not set")
		os.Exit(3)
	default:
		// Run the function and exit.
		err = instance.CommandLineOperation()
		if err != nil {
			fmt.Fprintf(os.Stderr, "command line operation failed: %s\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	}

	// Set default log level.
	log.SetLogLevel(log.WarningLevel)
	_ = log.Start()

	// Start
	go func() {
		err = instance.Start()
		if err != nil {
			fmt.Printf("instance start failed: %s\n", err)

			// Print stack on start failure, if enabled.
			if printStackOnExit {
				printStackTo(os.Stdout, "PRINTING STACK ON START FAILURE")
			}

			os.Exit(1)
		}
	}()

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
		}

	case <-instance.Stopped():
		log.Shutdown()
		os.Exit(instance.ExitCode())
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

	// Rapid unplanned disassembly after 3 minutes.
	go func() {
		time.Sleep(3 * time.Minute)
		printStackTo(os.Stderr, "PRINTING STACK - TAKING TOO LONG FOR SHUTDOWN")
		os.Exit(1)
	}()

	// Stop instance.
	if err := instance.Stop(); err != nil {
		slog.Error("failed to stop", "err", err)
	}
	log.Shutdown()

	// Print stack on shutdown, if enabled.
	if printStackOnExit {
		printStackTo(os.Stdout, "PRINTING STACK ON EXIT")
	}

	os.Exit(instance.ExitCode())
}

func printStackTo(writer io.Writer, msg string) {
	_, err := fmt.Fprintf(writer, "===== %s =====\n", msg)
	if err == nil {
		err = pprof.Lookup("goroutine").WriteTo(writer, 1)
	}
	if err != nil {
		slog.Error("failed to write stack trace", "err", err)
	}
}

func inputSignals(signalCh chan os.Signal) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "SIGHUP":
			signalCh <- syscall.SIGHUP
		case "SIGINT":
			signalCh <- syscall.SIGINT
		case "SIGQUIT":
			signalCh <- syscall.SIGQUIT
		case "SIGTERM":
			signalCh <- syscall.SIGTERM
		case "SIGUSR1":
			signalCh <- sigUSR1
		}
	}
}
