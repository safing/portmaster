package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/safing/portbase/container"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/formats/dsd"
	"github.com/safing/portmaster/updates"

	"github.com/spf13/cobra"
	"github.com/tevino/abool"
)

var (
	runningInConsole bool
	onWindows        = runtime.GOOS == "windows"

	childIsRunning    = abool.NewBool(false)
	shuttingDown      = make(chan struct{})
	shutdownInitiated = abool.NewBool(false)
	programEnded      = make(chan struct{})
)

// Options for starting component
type Options struct {
	Identifier        string
	AllowDownload     bool
	AllowHidingWindow bool
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
		return run(cmd, &Options{
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
		return run(cmd, &Options{
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
		return run(cmd, &Options{
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

func run(cmd *cobra.Command, opts *Options) error {
	defer close(programEnded)

	// get original arguments
	var args []string
	if len(os.Args) < 4 {
		return cmd.Help()
	}
	args = os.Args[3:]

	// adapt identifier
	if onWindows {
		opts.Identifier += ".exe"
	}

	// run
	tries := 0
	for {
		// normal execution
		tryAgain, err := execute(opts, args)
		if tryAgain && err == nil {
			continue
		}

		if err != nil {
			tries++
			fmt.Printf("%s execution of %s failed: %s\n", logPrefix, opts.Identifier, err)
		}
		if !tryAgain {
			break
		}
		if tries >= 5 {
			fmt.Printf("%s error seems to be permanent, giving up...\n", logPrefix)
			return err
		}
		fmt.Printf("%s trying again...\n", logPrefix)
	}

	fmt.Printf("%s %s completed successfully\n", logPrefix, opts.Identifier)
	return nil
}

func execute(opts *Options, args []string) (cont bool, err error) {
	file, err := getFile(opts)
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

	fmt.Printf("%s starting %s %s\n", logPrefix, file.Path(), strings.Join(args, " "))

	// log files
	var logFile, errorFile *os.File
	logFileBasePath := filepath.Join(*databaseRootDir, "logs", "fstree", strings.SplitN(opts.Identifier, "/", 2)[0])
	err = os.MkdirAll(logFileBasePath, 0777)
	if err != nil {
		fmt.Printf("%s failed to create log file folder %s: %s\n", logPrefix, logFileBasePath, err)
	} else {
		// open log file
		logFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.log", time.Now().Format("2006-02-01-15-04-05")))
		logFile = initializeLogFile(logFilePath, opts.Identifier, file)
		if logFile != nil {
			defer finalizeLogFile(logFile, logFilePath)
		}
		// open error log file
		errorFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.error.log", time.Now().Format("2006-02-01-15-04-05")))
		errorFile = initializeLogFile(errorFilePath, opts.Identifier, file)
		if errorFile != nil {
			defer finalizeLogFile(errorFile, errorFilePath)
		}
	}

	// create command
	exc := exec.Command(file.Path(), args...)

	if !runningInConsole && opts.AllowHidingWindow {
		// Windows only:
		// only hide (all) windows of program if we are not running in console and windows may be hidden
		hideWindow(exc)
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

	// start
	err = exc.Start()
	if err != nil {
		return true, fmt.Errorf("failed to start %s: %s", opts.Identifier, err)
	}
	childIsRunning.Set()

	// start output writers
	go func() {
		var logFileError error
		if logFile == nil {
			_, logFileError = io.Copy(os.Stdout, stdout)
		} else {
			_, logFileError = io.Copy(io.MultiWriter(os.Stdout, logFile), stdout)
		}
		if logFileError != nil {
			fmt.Printf("%s failed write logs: %s\n", logPrefix, logFileError)
		}
	}()
	go func() {
		var errorFileError error
		if logFile == nil {
			_, errorFileError = io.Copy(os.Stderr, stderr)
		} else {
			_, errorFileError = io.Copy(io.MultiWriter(os.Stderr, errorFile), stderr)
		}
		if errorFileError != nil {
			fmt.Printf("%s failed write error logs: %s\n", logPrefix, errorFileError)
		}
	}()
	// give some time to finish log file writing
	defer func() {
		time.Sleep(100 * time.Millisecond)
		childIsRunning.UnSet()
	}()

	// wait for completion
	finished := make(chan error)
	go func() {
		finished <- exc.Wait()
		close(finished)
	}()

	// state change listeners
	for {
		select {
		case <-shuttingDown:
			err := exc.Process.Signal(os.Interrupt)
			if err != nil {
				fmt.Printf("%s failed to signal %s to shutdown: %s\n", logPrefix, opts.Identifier, err)
				fmt.Printf("%s forcing shutdown...\n", logPrefix)
				// wait until shut down
				<-finished
				return false, nil
			}
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
					case 2357427: // Leet Speak for "restart"
						// restart request
						fmt.Printf("%s restarting %s\n", logPrefix, opts.Identifier)
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

func initiateShutdown() {
	if shutdownInitiated.SetToIf(false, true) {
		close(shuttingDown)
	}
}

func initializeLogFile(logFilePath string, identifier string, updateFile *updates.File) *os.File {
	logFile, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		fmt.Printf("%s failed to create log file %s: %s\n", logPrefix, logFilePath, err)
		return nil
	}

	// create header, so that the portmaster can view log files as a database
	meta := record.Meta{}
	meta.Update()
	meta.SetAbsoluteExpiry(time.Now().Add(720 * time.Hour).Unix()) // one month

	// manually marshal
	// version
	c := container.New([]byte{1})
	// meta
	metaSection, err := dsd.Dump(meta, dsd.JSON)
	if err != nil {
		fmt.Printf("%s failed to serialize header for log file %s: %s\n", logPrefix, logFilePath, err)
		finalizeLogFile(logFile, logFilePath)
		return nil
	}
	c.AppendAsBlock(metaSection)
	// log file data type (string) and newline for better manual viewing
	c.Append([]byte("S\n"))
	c.Append([]byte(fmt.Sprintf("executing %s version %s on %s %s\n", identifier, updateFile.Version(), runtime.GOOS, runtime.GOARCH)))

	_, err = logFile.Write(c.CompileData())
	if err != nil {
		fmt.Printf("%s failed to write header for log file %s: %s\n", logPrefix, logFilePath, err)
		finalizeLogFile(logFile, logFilePath)
		return nil
	}

	return logFile
}

func finalizeLogFile(logFile *os.File, logFilePath string) {
	err := logFile.Close()
	if err != nil {
		fmt.Printf("%s failed to close log file %s: %s\n", logPrefix, logFilePath, err)
	}

	//keep := true
	// check file size
	stat, err := os.Stat(logFilePath)
	if err == nil {
		// delete again if file is smaller than
		if stat.Size() < 200 { // header + info is about 150 bytes
			// keep = false
			err := os.Remove(logFilePath)
			if err != nil {
				fmt.Printf("%s failed to delete empty log file %s: %s\n", logPrefix, logFilePath, err)
			}
		}
	}

	//if !keep {
	//	err := os.Remove(logFilePath)
	//	if err != nil {
	//		fmt.Printf("%s failed to delete empty log file %s: %s\n", logPrefix, logFilePath, err)
	//	}
	//}
}
