package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/tevino/abool"
)

const (
	restartCode = 23
)

var (
	runningInConsole bool
	onWindows        = runtime.GOOS == "windows"

	childIsRunning = abool.NewBool(false)
)

// Options for starting component
type Options struct {
	Identifier        string // component identifier
	ShortIdentifier   string // populated automatically
	SuppressArgs      bool   // do not use any args
	AllowDownload     bool   // allow download of component if it is not yet available
	AllowHidingWindow bool   // allow hiding the window of the subprocess
	NoOutput          bool   // do not use stdout/err if logging to file is available (did not fail to open log file)
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.AddCommand(runCore)
	runCmd.AddCommand(runApp)
	runCmd.AddCommand(runNotifier)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a Portmaster component in the foreground",
}

var runCore = &cobra.Command{
	Use:   "core",
	Short: "Run the Portmaster Core",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleRun(cmd, &Options{
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

var runApp = &cobra.Command{
	Use:   "app",
	Short: "Run the Portmaster App",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleRun(cmd, &Options{
			Identifier:        "app/portmaster-app",
			AllowDownload:     false,
			AllowHidingWindow: false,
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

var runNotifier = &cobra.Command{
	Use:   "notifier",
	Short: "Run the Portmaster Notifier",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleRun(cmd, &Options{
			Identifier:        "notifier/portmaster-notifier",
			AllowDownload:     false,
			AllowHidingWindow: true,
		})
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

func handleRun(cmd *cobra.Command, opts *Options) (err error) {
	err = run(cmd, opts)
	initiateShutdown(err)
	return
}

func run(cmd *cobra.Command, opts *Options) (err error) {

	// set download option
	if opts.AllowDownload {
		registry.Online = true
	}

	// parse identifier
	opts.ShortIdentifier = path.Dir(opts.Identifier)

	// check for concurrent error (eg. service)
	shutdownLock.Lock()
	alreadyDead := shutdownInitiated
	shutdownLock.Unlock()
	if alreadyDead {
		return
	}

	// check for duplicate instances
	if opts.ShortIdentifier == "core" {
		pid, _ := checkAndCreateInstanceLock(opts.ShortIdentifier)
		if pid != 0 {
			return fmt.Errorf("another instance of Portmaster Core is already running: PID %d", pid)
		}
		defer func() {
			err := deleteInstanceLock(opts.ShortIdentifier)
			if err != nil {
				log.Printf("failed to delete instance lock: %s\n", err)
			}
		}()

	}

	// notify service after some time
	go func() {
		// assume that after 3 seconds service has finished starting
		time.Sleep(3 * time.Second)
		startupComplete <- struct{}{}
	}()

	// get original arguments
	var args []string
	if len(os.Args) < 4 {
		return cmd.Help()
	}
	args = os.Args[3:]
	if opts.SuppressArgs {
		args = nil
	}

	// adapt identifier
	if onWindows {
		opts.Identifier += ".exe"
	}

	// setup logging
	// init log file
	logFile := initControlLogFile()
	if logFile != nil {
		// don't close logFile, will be closed by system
		if opts.NoOutput {
			log.Println("disabling log output to stdout... bye!")
			log.SetOutput(logFile)
		} else {
			log.SetOutput(io.MultiWriter(os.Stdout, logFile))
		}
	}

	// run
	tries := 0
	for {
		// normal execution
		tryAgain := false
		tryAgain, err = execute(opts, args)
		switch {
		case tryAgain && err != nil:
			// temporary? execution error
			log.Printf("execution of %s failed: %s\n", opts.Identifier, err)
			tries++
			if tries >= 5 {
				log.Println("error seems to be permanent, giving up...")
				return err
			}
			log.Println("trying again...")
		case tryAgain && err == nil:
			// upgrade
			log.Println("restarting by request...")
		case !tryAgain && err != nil:
			// fatal error
			return err
		case !tryAgain && err == nil:
			// clean exit
			log.Printf("%s completed successfully\n", opts.Identifier)
			return nil
		}
	}
}

// nolint:gocyclo,gocognit // TODO: simplify
func execute(opts *Options, args []string) (cont bool, err error) {
	file, err := registry.GetFile(platform(opts.Identifier))
	if err != nil {
		return true, fmt.Errorf("could not get component: %s", err)
	}

	// check permission
	if !onWindows {
		info, err := os.Stat(file.Path())
		if err != nil {
			return true, fmt.Errorf("failed to get file info on %s: %s", file.Path(), err)
		}
		if info.Mode() != 0755 {
			err := os.Chmod(file.Path(), 0755)
			if err != nil {
				return true, fmt.Errorf("failed to set exec permissions on %s: %s", file.Path(), err)
			}
		}
	}

	log.Printf("starting %s %s\n", file.Path(), strings.Join(args, " "))

	// log files
	var logFile, errorFile *os.File
	logFileBasePath := filepath.Join(logsRoot.Path, "fstree", opts.ShortIdentifier)
	err = logsRoot.EnsureAbsPath(logFileBasePath)
	if err != nil {
		log.Printf("failed to check/create log file dir %s: %s\n", logFileBasePath, err)
	} else {
		// open log file
		logFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.log", time.Now().UTC().Format("2006-02-01-15-04-05")))
		logFile = initializeLogFile(logFilePath, opts.Identifier, file.Version())
		if logFile != nil {
			defer finalizeLogFile(logFile, logFilePath)
		}
		// open error log file
		errorFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.error.log", time.Now().UTC().Format("2006-02-01-15-04-05")))
		errorFile = initializeLogFile(errorFilePath, opts.Identifier, file.Version())
		if errorFile != nil {
			defer finalizeLogFile(errorFile, errorFilePath)
		}
	}

	// create command
	exc := exec.Command(file.Path(), args...) //nolint:gosec // everything is okay

	if !runningInConsole && opts.AllowHidingWindow {
		// Windows only:
		// only hide (all) windows of program if we are not running in console and windows may be hidden
		hideWindow(exc)
	}

	// check if input signals are enabled
	inputSignalsEnabled := false
	for _, arg := range args {
		if strings.HasSuffix(arg, "-input-signals") {
			inputSignalsEnabled = true
			break
		}
	}

	// consume stdout/stderr
	stdout, err := exc.StdoutPipe()
	if err != nil {
		return true, fmt.Errorf("failed to connect stdout: %s", err)
	}
	stderr, err := exc.StderrPipe()
	if err != nil {
		return true, fmt.Errorf("failed to connect stderr: %s", err)
	}
	var stdin io.WriteCloser
	if inputSignalsEnabled {
		stdin, err = exc.StdinPipe()
		if err != nil {
			return true, fmt.Errorf("failed to connect stdin: %s", err)
		}
	}

	// start
	err = exc.Start()
	if err != nil {
		return true, fmt.Errorf("failed to start %s: %s", opts.Identifier, err)
	}
	childIsRunning.Set()

	// start output writers
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		var logFileError error
		if logFile == nil {
			_, logFileError = io.Copy(os.Stdout, stdout)
		} else {
			if opts.NoOutput {
				_, logFileError = io.Copy(logFile, stdout)
			} else {
				_, logFileError = io.Copy(io.MultiWriter(os.Stdout, logFile), stdout)
			}
		}
		if logFileError != nil {
			log.Printf("failed write logs: %s\n", logFileError)
		}
		wg.Done()
	}()
	go func() {
		var errorFileError error
		if logFile == nil {
			_, errorFileError = io.Copy(os.Stderr, stderr)
		} else {
			if opts.NoOutput {
				_, errorFileError = io.Copy(errorFile, stderr)
			} else {
				_, errorFileError = io.Copy(io.MultiWriter(os.Stderr, errorFile), stderr)
			}
		}
		if errorFileError != nil {
			log.Printf("failed write error logs: %s\n", errorFileError)
		}
		wg.Done()
	}()

	// wait for completion
	finished := make(chan error)
	go func() {
		// wait for output writers to complete
		wg.Wait()
		// wait for process to return
		finished <- exc.Wait()
		// update status
		childIsRunning.UnSet()
		// notify manager
		close(finished)
	}()

	// state change listeners
	for {
		select {
		case <-shuttingDown:
			// signal process shutdown
			if inputSignalsEnabled {
				// for windows
				_, err = stdin.Write([]byte("SIGINT\n"))
			} else {
				err = exc.Process.Signal(os.Interrupt)
			}
			if err != nil {
				log.Printf("failed to signal %s to shutdown: %s\n", opts.Identifier, err)
				err = exc.Process.Kill()
				if err != nil {
					return false, fmt.Errorf("failed to kill %s: %s", opts.Identifier, err)
				}
				return false, fmt.Errorf("killed %s", opts.Identifier)
			}
			// wait until shut down
			select {
			case <-finished:
			case <-time.After(11 * time.Second): // portmaster core prints stack if not able to shutdown in 10 seconds
				// kill
				err = exc.Process.Kill()
				if err != nil {
					return false, fmt.Errorf("failed to kill %s: %s", opts.Identifier, err)
				}
				return false, fmt.Errorf("killed %s", opts.Identifier)
			}
			return false, nil
		case err := <-finished:
			if err != nil {
				exErr, ok := err.(*exec.ExitError)
				if ok {
					switch exErr.ProcessState.ExitCode() {
					case 0:
						// clean exit
						return false, fmt.Errorf("clean exit, but with error: %s", err)
					case 1:
						// error exit
						return true, fmt.Errorf("error during execution: %s", err)
					case restartCode:
						// restart request
						log.Printf("restarting %s\n", opts.Identifier)
						return true, nil
					default:
						return true, fmt.Errorf("unexpected error during execution: %s", err)
					}
				} else {
					return true, fmt.Errorf("unexpected error type during execution: %s", err)
				}
			}
			// clean exit
			return false, nil
		}
	}
}
