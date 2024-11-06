package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/pprof"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/mgr"
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

func cmdRun(cmd *cobra.Command, args []string) {
	// Call platform specific checks, that will execute commands like "recover-iptables"
	platformSpecificChecks()

	svcCfg.VerifyBinaryUpdates = nil // FIXME
	svcCfg.VerifyIntelUpdates = nil  // FIXME

	instance := createInstance()
	run(instance)
}

func createInstance() *service.Instance {
	// enable SPN client mode
	conf.EnableClient(true)
	conf.EnableIntegration(true)

	// Create instance.
	var execCmdLine bool
	instance, err := service.New(svcCfg)
	switch {
	case err == nil:
		// Continue
	case errors.Is(err, mgr.ErrExecuteCmdLineOp):
		execCmdLine = true
	default:
		fmt.Printf("error creating an instance: %s\n", err)
		os.Exit(2)
	}

	// Execute module command line operation, if requested or available.
	switch {
	case !execCmdLine:
		// Run service.
	case instance.CommandLineOperation == nil:
		fmt.Println("command line operation execution requested, but not set")
		os.Exit(3)
	default:
		// Run the function and exit.
		fmt.Println("executing cmdline op")
		err = instance.CommandLineOperation()
		if err != nil {
			fmt.Fprintf(os.Stderr, "command line operation failed: %s\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	}
	return instance
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
