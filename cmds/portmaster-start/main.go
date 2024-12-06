package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/info"
	portlog "github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/updates/helper"
)

var (
	dataDir    string
	maxRetries int
	dataRoot   *utils.DirStructure
	logsRoot   *utils.DirStructure
	forceOldUI bool

	updateURLFlag string
	userAgentFlag string

	// Create registry.
	registry = &updater.ResourceRegistry{
		Name: "updates",
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		UserAgent:    fmt.Sprintf("Portmaster Start (%s %s)", runtime.GOOS, runtime.GOARCH),
		Verification: helper.VerificationConfig,
		DevMode:      false,
		Online:       true, // is disabled later based on command
	}

	rootCmd = &cobra.Command{
		Use:   "portmaster-start",
		Short: "Start Portmaster components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
			mustLoadIndex := indexRequired(cmd)
			if err := configureRegistry(mustLoadIndex); err != nil {
				return err
			}

			if err := ensureLoggingDir(); err != nil {
				return err
			}

			return nil
		},
		SilenceUsage: true,
	}
)

func init() {
	// Let cobra ignore if we are running as "GUI" or not
	cobra.MousetrapHelpText = ""

	flags := rootCmd.PersistentFlags()
	{
		flags.StringVar(&dataDir, "data", "", "Configures the data directory. Alternatively, this can also be set via the environment variable PORTMASTER_DATA.")
		flags.StringVar(&updateURLFlag, "update-server", "", "Set an alternative update server (full URL)")
		flags.StringVar(&userAgentFlag, "update-agent", "", "Set an alternative user agent for requests to the update server")
		flags.IntVar(&maxRetries, "max-retries", 5, "Maximum number of retries when starting a Portmaster component")
		flags.BoolVar(&stdinSignals, "input-signals", false, "Emulate signals using stdin.")
		flags.BoolVar(&forceOldUI, "old-ui", false, "Use the old ui. (Beta)")
		_ = rootCmd.MarkPersistentFlagDirname("data")
		_ = flags.MarkHidden("input-signals")
	}
}

func main() {
	cobra.OnInitialize(initCobra)

	// set meta info
	info.Set("Portmaster Start", "", "GPLv3")

	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal, 2)
	signal.Notify(
		signalCh,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// start root command
	go func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()

	// wait for signals
	for sig := range signalCh {
		if childIsRunning.IsSet() {
			log.Printf("got %s signal (ignoring), waiting for child to exit...\n", sig)
			continue
		}

		log.Printf("got %s signal, exiting... (not executing anything)\n", sig)
		os.Exit(0)
	}
}

func initCobra() {
	// check if we are running in a console (try to attach to parent console if available)
	var err error
	runningInConsole, err = attachToParentConsole()
	if err != nil {
		log.Fatalf("failed to attach to parent console: %s\n", err)
	}

	// check if meta info is ok
	err = info.CheckVersion()
	if err != nil {
		log.Fatalf("compile error: please compile using the provided build script")
	}

	// set up logging
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("[pmstart] ")
	log.SetOutput(os.Stdout)

	// not using portbase logger
	portlog.SetLogLevel(portlog.CriticalLevel)
}

func configureRegistry(mustLoadIndex bool) error {
	// Check if update server URL supplied via flag is a valid URL.
	if updateURLFlag != "" {
		u, err := url.Parse(updateURLFlag)
		if err != nil {
			return fmt.Errorf("supplied update server URL is invalid: %w", err)
		}
		if u.Scheme != "https" {
			return errors.New("supplied update server URL must use HTTPS")
		}
	}

	// Override values from flags.
	if userAgentFlag != "" {
		registry.UserAgent = userAgentFlag
	}
	if updateURLFlag != "" {
		registry.UpdateURLs = []string{updateURLFlag}
	}

	// If dataDir is not set, check the environment variable.
	if dataDir == "" {
		dataDir = os.Getenv("PORTMASTER_DATA")
	}

	// If it's still empty, try to auto-detect it.
	if dataDir == "" {
		dataDir = detectInstallationDir()
	}

	// Finally, if it's still empty, the user must provide it.
	if dataDir == "" {
		return errors.New("please set the data directory using --data=/path/to/data/dir")
	}

	// Remove left over quotes.
	dataDir = strings.Trim(dataDir, `\"`)
	// Initialize data root.
	err := dataroot.Initialize(dataDir, utils.PublicReadPermission)
	if err != nil {
		return fmt.Errorf("failed to initialize data root: %w", err)
	}
	dataRoot = dataroot.Root()

	// Initialize registry.
	err = registry.Initialize(dataRoot.ChildDir("updates", utils.PublicReadPermission))
	if err != nil {
		return err
	}

	return updateRegistryIndex(mustLoadIndex)
}

func ensureLoggingDir() error {
	// set up logs root
	logsRoot = dataRoot.ChildDir("logs", utils.PublicWritePermission)
	err := logsRoot.Ensure()
	if err != nil {
		return fmt.Errorf("failed to initialize logs root (%q): %w", logsRoot.Path, err)
	}

	// warn about CTRL-C on windows
	if runningInConsole && onWindows {
		log.Println("WARNING: portmaster-start is marked as a GUI application in order to get rid of the console window.")
		log.Println("WARNING: CTRL-C will immediately kill without clean shutdown.")
	}
	return nil
}

func updateRegistryIndex(mustLoadIndex bool) error {
	// Set indexes based on the release channel.
	warning := helper.SetIndexes(registry, "", false, false, false)
	if warning != nil {
		log.Printf("WARNING: %s\n", warning)
	}

	// Load indexes from disk or network, if needed and desired.
	err := registry.LoadIndexes(context.Background())
	if err != nil {
		log.Printf("WARNING: error loading indexes: %s\n", err)
		if mustLoadIndex {
			return err
		}
	}

	// Load versions from disk to know which others we have and which are available.
	err = registry.ScanStorage("")
	if err != nil {
		log.Printf("WARNING: error during storage scan: %s\n", err)
	}

	registry.SelectVersions()
	return nil
}

func detectInstallationDir() string {
	exePath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return ""
	}

	parent := filepath.Dir(exePath)
	stableJSONFile := filepath.Join(parent, "updates", "stable.json")
	stat, err := os.Stat(stableJSONFile)
	if err != nil {
		return ""
	}

	if stat.IsDir() {
		return ""
	}

	return parent
}
