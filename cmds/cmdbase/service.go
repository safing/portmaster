package cmdbase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/mgr"
)

var (
	RebootOnRestart  bool
	PrintStackOnExit bool
)

type SystemService interface {
	Run()
	IsService() bool
	RestartService() error
}

type ServiceInstance interface {
	Ready() bool
	Start() error
	Stop() error
	Restart()
	Shutdown()
	Ctx() context.Context
	IsShuttingDown() bool
	ShuttingDown() <-chan struct{}
	ShutdownCtx() context.Context
	IsShutDown() bool
	ShutdownComplete() <-chan struct{}
	ExitCode() int
	ShouldRestartIsSet() bool
	CommandLineOperationIsSet() bool
	CommandLineOperationExecute() error
}

var (
	SvcFactory func(*service.ServiceConfig) (ServiceInstance, error)
	SvcConfig  *service.ServiceConfig
)

func RunService(cmd *cobra.Command, args []string) {
	if SvcFactory == nil || SvcConfig == nil {
		fmt.Fprintln(os.Stderr, "internal error: service not set up in cmdbase")
		os.Exit(1)
	}

	// Start logging.
	// Note: Must be created before the service instance, so that they use the right logger.
	err := log.Start(SvcConfig.LogLevel, SvcConfig.LogToStdout, SvcConfig.LogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(4)
	}

	// Create instance.
	// Instance modules might request a cmdline execution of a function.
	var execCmdLine bool
	instance, err := SvcFactory(SvcConfig)
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
	case !instance.CommandLineOperationIsSet():
		fmt.Println("command line operation execution requested, but not set")
		os.Exit(3)
	default:
		// Run the function and exit.
		fmt.Println("executing cmdline op")
		err = instance.CommandLineOperationExecute()
		if err != nil {
			fmt.Fprintf(os.Stderr, "command line operation failed: %s\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	}

	// START

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
		if PrintStackOnExit {
			printStackTo(log.GlobalWriter, "PRINTING STACK ON EXIT")
		}
	case <-time.After(3 * time.Minute):
		printStackTo(log.GlobalWriter, "PRINTING STACK - TAKING TOO LONG FOR SHUTDOWN")
	}

	// Check if restart was triggered and send start service command if true.
	if instance.ShouldRestartIsSet() && service.IsService() {
		// Check if we should reboot instead.
		var rebooting bool
		if RebootOnRestart {
			// Trigger system reboot and record success.
			rebooting = triggerSystemReboot()
			if !rebooting {
				log.Warningf("updates: rebooting failed, only restarting service instead")
			}
		}

		// Restart service if not rebooting.
		if !rebooting {
			if err := service.RestartService(); err != nil {
				slog.Error("failed to restart service", "err", err)
			}
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

func triggerSystemReboot() (success bool) {
	switch runtime.GOOS {
	case "linux":
		err := exec.Command("systemctl", "reboot").Run()
		if err != nil {
			log.Errorf("updates: triggering reboot with systemctl failed: %s", err)
			return false
		}
	default:
		log.Warningf("updates: rebooting is not support on %s", runtime.GOOS)
		return false
	}

	return true
}
