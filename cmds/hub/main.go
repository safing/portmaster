package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/cmds/cmdbase"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn"
	"github.com/safing/portmaster/spn/conf"
)

var (
	rootCmd = &cobra.Command{
		Use:              "spn-hub",
		PersistentPreRun: initializeGlobals,
		Run:              cmdbase.RunService,
	}

	binDir  string
	dataDir string

	logToStdout bool
	logDir      string
	logLevel    string
)

func init() {
	// Add persistent flags for all commands.
	rootCmd.PersistentFlags().StringVar(&binDir, "bin-dir", "", "set directory for executable binaries (rw/ro)")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "set directory for variable data (rw)")

	// Add flags for service only.
	rootCmd.Flags().BoolVar(&logToStdout, "log-stdout", false, "log to stdout instead of file")
	rootCmd.Flags().StringVar(&logDir, "log-dir", "", "set directory for logs")
	rootCmd.Flags().StringVar(&logLevel, "log", "", "set log level to [trace|debug|info|warning|error|critical]")
	rootCmd.Flags().BoolVar(&cmdbase.PrintStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
	rootCmd.Flags().BoolVar(&cmdbase.RebootOnRestart, "reboot-on-restart", false, "reboot server instead of service restart")

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

func initializeGlobals(cmd *cobra.Command, args []string) {
	// Set name and license.
	info.Set("SPN Hub", "0.7.8", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("hub")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("SPN Hub (%s %s)", runtime.GOOS, runtime.GOARCH)

	// Set SPN public hub mode.
	conf.EnablePublicHub(true)

	// Set default log level.
	log.SetLogLevel(log.WarningLevel)
	_ = log.Start(log.InfoLevel.String(), true, "")

	// Configure SPN binary updates.
	configure.DefaultBinaryIndexName = "SPN Binaries"
	configure.DefaultStableBinaryIndexURLs = []string{
		"https://updates.safing.io/spn-stable.v3.json",
	}
	configure.DefaultBetaBinaryIndexURLs = []string{
		"https://updates.safing.io/spn-beta.v3.json",
	}
	configure.DefaultStagingBinaryIndexURLs = []string{
		"https://updates.safing.io/spn-staging.v3.json",
	}
	configure.DefaultSupportBinaryIndexURLs = []string{
		"https://updates.safing.io/spn-support.v3.json",
	}

	binDir = "/opt/safing/spn"
	dataDir = "/opt/safing/spn"

	// Configure service.
	cmdbase.SvcFactory = func(svcCfg *service.ServiceConfig) (cmdbase.ServiceInstance, error) {
		svc, err := spn.New(svcCfg)
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
	}
}
