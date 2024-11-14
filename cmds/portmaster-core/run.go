package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/pprof"
	"time"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

var printStackOnExit bool

func init() {
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
}

type SystemService interface {
	Run()
	IsService() bool
	RestartService() error
}

func cmdRun(cmd *cobra.Command, args []string) {
	// Run platform specific setup or switches.
	runPlatformSpecifics(cmd, args)

	// SETUP

	// Enable SPN client mode.
	// TODO: Move this to service config.
	conf.EnableClient(true)
	conf.EnableIntegration(true)

	// Create instance.
	// Instance modules might request a cmdline execution of a function.
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

	// START

	// FIXME: fix color and duplicate level when logging with slog
	// FIXME: check for tty for color enabling

	// Start logging.
	err = log.Start(svcCfg.LogLevel, svcCfg.LogToStdout, svcCfg.LogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(4)
	}

	// Create system service.
	service := NewSystemService(instance)

	// Start instance via system service manager.
	go func() {
		service.Run()
	}()

	// SHUTDOWN

	// Wait for shutdown to be started.
	<-instance.ShuttingDown()

	// Wait for shutdown to be finished.
	select {
	case <-instance.ShutdownComplete():
		// Print stack on shutdown, if enabled.
		if printStackOnExit {
			printStackTo(log.GlobalWriter, "PRINTING STACK ON EXIT")
		}
	case <-time.After(3 * time.Minute):
		printStackTo(log.GlobalWriter, "PRINTING STACK - TAKING TOO LONG FOR SHUTDOWN")
	}

	// Check if restart was triggered and send start service command if true.
	if instance.ShouldRestart && service.IsService() {
		if err := service.RestartService(); err != nil {
			slog.Error("failed to restart service", "err", err)
		}
	}

	// Stop logging.
	log.Shutdown()

	// Give a small amount of time for everything to settle:
	// - All logs written.
	// - Restart command started, if needed.
	// - Windows service manager notified.
	time.Sleep(100 * time.Millisecond)

	// Exit
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
