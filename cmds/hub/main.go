package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/info"
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
		Use:               "spn-hub",
		PersistentPreRunE: initializeGlobals,
		Run:               cmdbase.RunService,
	}

	binDir  string
	dataDir string

	logToStdout bool
	logDir      string
	logLevel    string

	errUnsafeDirLayout = errors.New("unsafe directory layout")
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

func initializeGlobals(cmd *cobra.Command, args []string) error {
	// Set name and license.
	info.Set("SPN Hub", "0.7.8", "GPLv3")

	// Configure metrics.
	_ = metrics.SetNamespace("hub")

	// Configure user agent.
	updates.UserAgent = fmt.Sprintf("SPN Hub (%s %s)", runtime.GOOS, runtime.GOARCH)

	// Set SPN public hub mode.
	conf.EnablePublicHub(true)

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

	if dataDir == "" {
		dataDir = "/opt/safing/spn"
	}
	// The binary updater and the logger each clean up their own directory,
	// so by default neither of them shares a directory with the data.
	if binDir == "" {
		binDir = filepath.Join(dataDir, "bin")
	}
	if logDir == "" && !logToStdout {
		logDir = filepath.Join(dataDir, "logs")
	}

	// The log dir is only cleaned up when logging to files.
	usedLogDir := logDir
	if logToStdout {
		usedLogDir = ""
	}
	// The running program may be somewhere else than in the bin dir.
	exeDir := ""
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot check the log dir against the location of the program: %s\n", err)
	} else {
		exeDir = filepath.Dir(exePath)
	}
	err = checkDirs(binDir, dataDir, usedLogDir, exeDir)
	if err != nil {
		return err
	}

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
		VerifyIntelUpdates:  configure.IntelSigningTrustStore,
	}

	return nil
}

// checkDirs rejects directory layouts in which the hub would remove its own files.
// The binary updater moves everything out of the bin dir when it upgrades, and
// the logger deletes old files from the log dir when it starts.
// An empty logDir means that no log dir is used. An empty exeDir means that the
// directory of the running program is not known.
func checkDirs(binDir, dataDir, logDir, exeDir string) error {
	binDir, dataDir = resolveDir(binDir), resolveDir(dataDir)
	if isSameOrWithin(dataDir, binDir) {
		return fmt.Errorf("%w: data dir %q must not be the bin dir %q or inside of it", errUnsafeDirLayout, dataDir, binDir)
	}

	if logDir == "" {
		return nil
	}
	logDir = resolveDir(logDir)
	switch {
	case logDir == dataDir:
		return fmt.Errorf("%w: log dir %q must not be the data dir", errUnsafeDirLayout, logDir)
	case isSameOrWithin(logDir, binDir):
		return fmt.Errorf("%w: log dir %q must not be the bin dir %q or inside of it", errUnsafeDirLayout, logDir, binDir)
	case exeDir != "" && logDir == resolveDir(exeDir):
		return fmt.Errorf("%w: log dir %q must not be the directory of the running program", errUnsafeDirLayout, logDir)
	}
	return nil
}

// resolveDir returns the absolute path of dir with symlinks resolved.
// The directory may not exist yet: symlinks are resolved in the part of the
// path that exists, and the rest is appended unchanged.
func resolveDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}

	missing := ""
	for existing := abs; ; {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(resolved, missing)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			// Not even the root can be resolved.
			return abs
		}
		missing = filepath.Join(filepath.Base(existing), missing)
		existing = parent
	}
}

// isSameOrWithin reports whether path is dir or lies inside of dir.
func isSameOrWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && filepath.IsLocal(rel)
}
