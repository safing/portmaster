//nolint:gci,nolintlint
package main

import (
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
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"

	// Include packages here.
	_ "github.com/safing/portmaster/service/core"
	_ "github.com/safing/portmaster/service/firewall"
	_ "github.com/safing/portmaster/service/nameserver"
	_ "github.com/safing/portmaster/service/ui"
	_ "github.com/safing/portmaster/spn/captain"
)

var sigUSR1 = syscall.Signal(0xa)

func main() {
	flag.Parse()

	// set information
	info.Set("Portmaster", "", "GPLv3")

	// Set default log level.
	log.SetLogLevel(log.WarningLevel)
	_ = log.Start()

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("Portmaster Core (%s %s)", runtime.GOOS, runtime.GOARCH)

	// enable SPN client mode
	conf.EnableClient(true)

	// Prep
	err := base.GlobalPrep()
	if err != nil {
		fmt.Printf("global prep failed: %s\n", err)
		return
	}

	// Create instance.
	instance, err := service.New(&service.ServiceConfig{})
	if err != nil {
		fmt.Printf("error creating an instance: %s\n", err)
		os.Exit(2)
	}

	// Execute command line operation, if available.
	if instance.CommandLineOperation != nil {
		// Run the function and exit.
		err = instance.CommandLineOperation()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cmdline operation failed: %s\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	}

	// Start
	go func() {
		err = instance.Start()
		if err != nil {
			fmt.Printf("instance start failed: %s\n", err)
			os.Exit(1)
		}
	}()

	// Wait for signal.
	signalCh := make(chan os.Signal, 1)
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
		slog.Error("failed to stop portmaster", "err", err)
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
