package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/info"
	portlog "github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/updates/helper"
)

var (
	dataDir    string
	staging    bool
	maxRetries int
	dataRoot   *utils.DirStructure
	logsRoot   *utils.DirStructure

	// Create registry.
	registry = &updater.ResourceRegistry{
		Name: "updates",
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
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
		flags.StringVar(&registry.UserAgent, "update-agent", "Start", "Sets the user agent for requests to the update server")
		flags.BoolVar(&staging, "staging", false, "Deprecated, configure in settings instead.")
		flags.IntVar(&maxRetries, "max-retries", 5, "Maximum number of retries when starting a Portmaster component")
		flags.BoolVar(&stdinSignals, "input-signals", false, "Emulate signals using stdin.")
		_ = rootCmd.MarkPersistentFlagDirname("data")
		_ = flags.MarkHidden("input-signals")
	}
}

func main() {
	cobra.OnInitialize(initCobra)

	// set meta info
	info.Set("Portmaster Start", "1.0.2", "AGPLv3", false)

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
	log.SetPrefix("[control] ")
	log.SetOutput(os.Stdout)

	// not using portbase logger
	portlog.SetLogLevel(portlog.CriticalLevel)
}

func configureRegistry(mustLoadIndex bool) error {
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
	err := dataroot.Initialize(dataDir, 0o0755)
	if err != nil {
		return fmt.Errorf("failed to initialize data root: %w", err)
	}
	dataRoot = dataroot.Root()

	// Initialize registry.
	err = registry.Initialize(dataRoot.ChildDir("updates", 0o0755))
	if err != nil {
		return err
	}

	return updateRegistryIndex(mustLoadIndex)
}

func ensureLoggingDir() error {
	// set up logs root
	logsRoot = dataRoot.ChildDir("logs", 0o0777)
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
	warning := helper.SetIndexes(registry, "", false)
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
