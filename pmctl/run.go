package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	runningInConsole bool
	onWindows        = runtime.GOOS == "windows"
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
	for {
		file, err := getFile(opts)
		if err != nil {
			return fmt.Errorf("could not get component: %s", err)
		}

		// check permission
		if !onWindows {
			info, err := os.Stat(file.Path())
			if err != nil {
				return fmt.Errorf("failed to get file info on %s: %s", file.Path(), err)
			}
			if info.Mode() != 0755 {
				err := os.Chmod(file.Path(), 0755)
				if err != nil {
					return fmt.Errorf("failed to set exec permissions on %s: %s", file.Path(), err)
				}
			}
		}

		fmt.Printf("%s starting %s %s\n", logPrefix, file.Path(), strings.Join(args, " "))

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
			return fmt.Errorf("failed to connect stdout: %s", err)
		}
		stderr, err := exc.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to connect stderr: %s", err)
		}

		// start
		err = exc.Start()
		if err != nil {
			return fmt.Errorf("failed to start %s: %s", opts.Identifier, err)
		}

		// start output writers
		go func() {
			io.Copy(os.Stdout, stdout)
		}()
		go func() {
			io.Copy(os.Stderr, stderr)
		}()

		// catch interrupt for clean shutdown
		signalCh := make(chan os.Signal)
		signal.Notify(
			signalCh,
			os.Interrupt,
			os.Kill,
			syscall.SIGHUP,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT,
		)
		go func() {
			for {
				sig := <-signalCh
				fmt.Printf("%s got %s signal (ignoring), waiting for %s to exit...\n", logPrefix, sig, opts.Identifier)
			}
		}()

		// wait for completion
		err = exc.Wait()
		if err != nil {
			exErr, ok := err.(*exec.ExitError)
			if ok {
				switch exErr.ProcessState.ExitCode() {
				case 0:
					// clean exit
					fmt.Printf("%s clean exit of %s, but with error: %s\n", logPrefix, opts.Identifier, err)
					os.Exit(1)
				case 1:
					// error exit
					fmt.Printf("%s error during execution of %s: %s\n", logPrefix, opts.Identifier, err)
					os.Exit(1)
				case 2357427: // Leet Speak for "restart"
					// restart request
					fmt.Printf("%s restarting %s\n", logPrefix, opts.Identifier)
					continue
				default:
					fmt.Printf("%s unexpected error during execution of %s: %s\n", logPrefix, opts.Identifier, err)
					os.Exit(exErr.ProcessState.ExitCode())
				}
			} else {
				fmt.Printf("%s unexpected error type during execution of %s: %s\n", logPrefix, opts.Identifier, err)
				os.Exit(1)
			}
		}
		// clean exit
		break
	}

	fmt.Printf("%s %s completed successfully\n", logPrefix, opts.Identifier)
	return nil
}
