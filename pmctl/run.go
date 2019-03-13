package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

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
		return run("core/portmaster", cmd, args)
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
		return run("app/portmaster-app", cmd, args)
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
		return run("notifier/portmaster-notifier", cmd, args)
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		// UnknownFlags will ignore unknown flags errors and continue parsing rest of the flags
		UnknownFlags: true,
	},
}

func run(identifier string, cmd *cobra.Command, args []string) error {

	if len(os.Args) <= 3 {
		return cmd.Help()
	}
	args = os.Args[3:]

	for {
		file, err := getFile(identifier)
		if err != nil {
			return fmt.Errorf("%s could not get component: %s", logPrefix, err)
		}

		// check permission
		info, err := os.Stat(file.Path())
		if info.Mode() != 0755 {
			err := os.Chmod(file.Path(), 0755)
			if err != nil {
				return fmt.Errorf("%s failed to set exec permissions on %s: %s", logPrefix, file.Path(), err)
			}
		}

		fmt.Printf("%s starting %s %s\n", logPrefix, file.Path(), strings.Join(args, " "))
		// os.Exit(0)

		// create command
		exc := exec.Command(file.Path(), args...)

		// consume stdout/stderr
		stdout, err := exc.StdoutPipe()
		if err != nil {
			return fmt.Errorf("%s failed to connect stdout: %s", logPrefix, err)
		}
		stderr, err := exc.StderrPipe()
		if err != nil {
			return fmt.Errorf("%s failed to connect stderr: %s", logPrefix, err)
		}

		// start
		err = exc.Start()
		if err != nil {
			return fmt.Errorf("%s failed to start %s: %s", logPrefix, identifier, err)
		}

		// start output writers
		go func() {
			io.Copy(os.Stdout, stdout)
		}()
		go func() {
			io.Copy(os.Stderr, stderr)
		}()

		// wait for completion
		err = exc.Wait()
		if err != nil {
			exErr, ok := err.(*exec.ExitError)
			if ok {
				switch exErr.ProcessState.ExitCode() {
				case 0:
					// clean exit
					fmt.Printf("%s clean exit of %s, but with error: %s\n", logPrefix, identifier, err)
					break
				case 1:
					// error exit
					fmt.Printf("%s error during execution of %s: %s\n", logPrefix, identifier, err)
					os.Exit(1)
				case 2:
					// restart request
					fmt.Printf("%s restarting %s\n", logPrefix, identifier)
					continue
				default:
					fmt.Printf("%s unexpected error during execution of %s: %s\n", logPrefix, identifier, err)
					os.Exit(1)
				}
			} else {
				fmt.Printf("%s unexpected error type during execution of %s: %s\n", logPrefix, identifier, err)
				os.Exit(1)
			}
		}

		// clean exit
		break
	}
	return nil
}
