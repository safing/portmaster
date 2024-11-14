package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/updates"
)

var (
	rootCmd = &cobra.Command{
		Use:              "portmaster-core",
		PersistentPreRun: initializeGlobals,
		Run:              cmdRun,
	}

	binDir  string
	dataDir string

	logToStdout bool
	logDir      string
	logLevel    string

	svcCfg *service.ServiceConfig
)

func init() {
	// Add Go's default flag set.
	// TODO: Move flags throughout Portmaster to here and add their values to the service config.
	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)

	// Add persisent flags for all commands.
	rootCmd.PersistentFlags().StringVar(&binDir, "bin-dir", "", "set directory for executable binaries (rw/ro)")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "set directory for variable data (rw)")

	// Add flags for service only.
	rootCmd.Flags().BoolVar(&logToStdout, "log-stdout", false, "log to stdout instead of file")
	rootCmd.Flags().StringVar(&logDir, "log-dir", "", "set directory for logs")
	rootCmd.Flags().StringVar(&logLevel, "log", "", "set log level to [trace|debug|info|warning|error|critical]")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func initializeGlobals(cmd *cobra.Command, args []string) {
	// set information
	info.Set("Portmaster", "", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("portmaster")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("Portmaster Core (%s %s)", runtime.GOOS, runtime.GOARCH)

	// Create service config.
	svcCfg = &service.ServiceConfig{
		BinDir:  binDir,
		DataDir: dataDir,

		LogToStdout: logToStdout,
		LogDir:      logDir,
		LogLevel:    logLevel,

		BinariesIndexURLs:   service.DefaultStableBinaryIndexURLs,
		IntelIndexURLs:      service.DefaultIntelIndexURLs,
		VerifyBinaryUpdates: service.BinarySigningTrustStore,
		VerifyIntelUpdates:  service.BinarySigningTrustStore,
	}
}
