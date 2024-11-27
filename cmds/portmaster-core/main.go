package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/cmds/cmdbase"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/updates"
)

var (
	rootCmd = &cobra.Command{
		Use:              "portmaster-core",
		PersistentPreRun: initializeGlobals,
		Run:              mainRun,
	}

	binDir  string
	dataDir string

	logToStdout bool
	logDir      string
	logLevel    string

	printVersion bool
)

func init() {
	// Add persisent flags for all commands.
	rootCmd.PersistentFlags().StringVar(&binDir, "bin-dir", "", "set directory for executable binaries (rw/ro)")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "set directory for variable data (rw)")

	// Add flags for service only.
	rootCmd.Flags().BoolVar(&logToStdout, "log-stdout", false, "log to stdout instead of file")
	rootCmd.Flags().StringVar(&logDir, "log-dir", "", "set directory for logs")
	rootCmd.Flags().StringVar(&logLevel, "log", "", "set log level to [trace|debug|info|warning|error|critical]")
	rootCmd.Flags().BoolVar(&printVersion, "version", false, "print version (backward compatibility; use command instead)")
	rootCmd.Flags().BoolVar(&cmdbase.PrintStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")

	// Add other commands.
	rootCmd.AddCommand(cmdbase.VersionCmd)
	rootCmd.AddCommand(cmdbase.UpdateCmd)
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

func mainRun(cmd *cobra.Command, args []string) {
	runPlatformSpecifics(cmd, args)
	cmdbase.RunService(cmd, args)
}

func initializeGlobals(cmd *cobra.Command, args []string) {
	// Set version info.
	info.Set("Portmaster", "", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("Portmaster Core (%s %s)", runtime.GOOS, runtime.GOARCH)

	// Configure service.
	cmdbase.SvcFactory = func(svcCfg *service.ServiceConfig) (cmdbase.ServiceInstance, error) {
		svc, err := service.New(svcCfg)
		return svc, err
	}
	cmdbase.SvcConfig = &service.ServiceConfig{
		BinDir:  binDir,
		DataDir: dataDir,

		LogToStdout: logToStdout,
		LogDir:      logDir,
		LogLevel:    logLevel,

		BinariesIndexURLs:   configure.DefaultStableBinaryIndexURLs,
		IntelIndexURLs:      configure.DefaultIntelIndexURLs,
		VerifyBinaryUpdates: configure.BinarySigningTrustStore,
		VerifyIntelUpdates:  configure.BinarySigningTrustStore,
	}
}

func runFlagCmd(fn func(cmd *cobra.Command, args []string) error, cmd *cobra.Command, args []string) {
	if err := fn(cmd, args); err != nil {
		fmt.Printf("failed: %s\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}
