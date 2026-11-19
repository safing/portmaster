package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/cmds/cmdbase"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/sluice"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:              "observation-hub",
		PersistentPreRun: initializeGlobals,
		Run:              cmdbase.RunService,
	}

	dataDir string

	logToStdout bool
	logDir      string
	logLevel    string

	errNoDataDir = errors.New("no data directory set, use --data-dir")
)

func init() {
	// Add persistent flags for all commands.
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "set directory for variable data (rw); required")

	// Add flags for service only.
	rootCmd.Flags().BoolVar(&logToStdout, "log-stdout", false, "log to stdout instead of file")
	rootCmd.Flags().StringVar(&logDir, "log-dir", "", "set directory for logs")
	rootCmd.Flags().StringVar(&logLevel, "log", "", "set log level to [trace|debug|info|warning|error|critical]")
	rootCmd.Flags().BoolVar(&cmdbase.PrintStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
	rootCmd.Flags().BoolVar(&cmdbase.RebootOnRestart, "reboot-on-restart", false, "reboot server instead of service restart")

	// Add other commands.
	rootCmd.AddCommand(cmdbase.VersionCmd)
}

func main() {
	// Add Go's default flag set.
	// TODO: Move flags throughout Portmaster to here and add their values to the service config.
	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func initializeGlobals(cmd *cobra.Command, args []string) {
	// Set version info.
	info.Set("SPN Observation Hub", "", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("observer")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("SPN Observation Hub (%s %s)", runtime.GOOS, runtime.GOARCH)

	// Configure SPN mode.
	conf.EnableClient(true)
	captain.DisableAccount = true

	// Disable unneeded listeners.
	sluice.EnableListener = false
	api.EnableServer = false

	// Configure service.
	cmdbase.SvcFactory = func(svcCfg *service.ServiceConfig) (cmdbase.ServiceInstance, error) {
		// Without this check the service falls back to Portmaster's own data directory.
		if svcCfg.DataDir == "" {
			return nil, errNoDataDir
		}

		// Return the error unchanged: cmdbase checks it for mgr.ErrExecuteCmdLineOp.
		return NewInstance(svcCfg)
	}
	// No binary directory and no binary index: the instance has no binary updater.
	cmdbase.SvcConfig = &service.ServiceConfig{
		DataDir: dataDir,

		LogToStdout: logToStdout,
		LogDir:      logDir,
		LogLevel:    logLevel,

		IntelIndexURLs:     configure.DefaultIntelIndexURLs,
		VerifyIntelUpdates: configure.IntelSigningTrustStore,
	}
}
